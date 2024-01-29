/*
Copyright 2024 Nokia.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package targetconfigserver

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	//"sigs.k8s.io/controller-runtime/pkg/log"
	"github.com/henderiw/logger/log"

	configv1alpha1 "github.com/iptecharch/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/configserver"
	"github.com/iptecharch/config-server/pkg/lease"
	"github.com/iptecharch/config-server/pkg/reconcilers"
	"github.com/iptecharch/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/iptecharch/config-server/pkg/reconcilers/resource"
	"github.com/iptecharch/config-server/pkg/store"
	"github.com/iptecharch/config-server/pkg/target"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
)

func init() {
	reconcilers.Register("targetconfigserver", &reconciler{})
}

const (
	finalizer = "targetconfigserver.inv.sdcio.dev/finalizer"
	// errors
	errGetCr           = "cannot get cr"
	errUpdateDataStore = "cannot update datastore"
	errUpdateStatus    = "cannot update status"
)

//+kubebuilder:rbac:groups=inv.sdcio.dev,resources=targets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=inv.sdcio.dev,resources=targets/status,verbs=get;update;patch

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {
	cfg, ok := c.(*ctrlconfig.ControllerConfig)
	if !ok {
		return nil, fmt.Errorf("cannot initialize, expecting controllerConfig, got: %s", reflect.TypeOf(c).Name())
	}

	/*
	if err := invv1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		return nil, err
	}
	*/

	r.Client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer)
	r.configProvider = cfg.ConfigProvider
	//r.targetTransitionStore = memory.NewStore[bool]() // keeps track of the target status locally
	r.targetStore = cfg.TargetStore

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named("TargetConfigServerController").
		For(&invv1alpha1.Target{}).
		Complete(r)
}

type reconciler struct {
	client.Client
	finalizer *resource.APIFinalizer

	configProvider configserver.ResourceProvider
	//targetTransitionStore store.Storer[bool] // keeps track of the target status locally
	targetStore store.Storer[target.Context]
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).With("req", req)
	log.Info("reconcile")

	targetKey := store.KeyFromNSN(req.NamespacedName)

	cr := &invv1alpha1.Target{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if resource.IgnoreNotFound(err) != nil {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}

	cr = cr.DeepCopy()

	if !cr.GetDeletionTimestamp().IsZero() {
		// list the configs per target
		cr.SetConditions(invv1alpha1.ConfigFailed("target deleting"))
		configList, err := r.listTargetConfigs(ctx, cr)
		if err != nil {
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		for _, config := range configList.Items {
			condition := config.GetCondition(configv1alpha1.ConditionTypeReady)
			if condition.Status != metav1.ConditionFalse && condition.Message != string(configv1alpha1.ConditionReasonTargetNotFound) {
				// update the status if not already set
				// resource version does not need to be updated
				config.SetConditions(configv1alpha1.Failed(string(configv1alpha1.ConditionReasonTargetNotFound)))
				r.configProvider.UpdateStore(ctx, store.KeyFromNSN(types.NamespacedName{
					Name:      config.GetName(),
					Namespace: config.GetNamespace(),
				}), &config)
			}
		}
		if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
			log.Error("cannot remove finalizer", "error", err)
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		log.Info("Successfully deleted resource")
		return ctrl.Result{}, nil
	}

	if err := r.finalizer.AddFinalizer(ctx, cr); err != nil {
		log.Error("cannot add finalizer", "error", err)
		return ctrl.Result{Requeue: true}, err
	}

	l := r.getLease(ctx, targetKey)
	if err := l.AcquireLease(ctx, "TargetConfigServerController"); err != nil {
		log.Info("cannot acquire lease", "error", err.Error())
		return ctrl.Result{Requeue: true, RequeueAfter: lease.RequeueInterval}, nil
	}

	// handle transition
	ready, tctx := r.GetTargetReadiness(ctx, targetKey, cr)
	log.Info("readiness", "ready", ready)
	if ready {
		cfgCondition := cr.GetCondition(invv1alpha1.ConditionTypeConfigReady)
		if cfgCondition.Status == metav1.ConditionFalse &&
			cfgCondition.Reason != string(invv1alpha1.ConditionReasonReApplyFailed) {

			log.Info("target reapply config")
			// we split the config in config that was successfully applied to config that was not yet
			priorityReApplyConfigs, reApplyConfigs, err := r.getReApplyConfigs(ctx, cr)
			if err != nil {
				cr.SetConditions(invv1alpha1.ConfigFailed(err.Error()))
				return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
			}

			for _, config := range priorityReApplyConfigs {
				log.Info("target reapply config", "priority", config.Name)
			}
			for _, config := range priorityReApplyConfigs {
				log.Info("target reapply config", "regular", config.Name)
			}

			// We need to restore the config on the target
			for _, config := range priorityReApplyConfigs {
				if err := r.configProvider.SetIntent(ctx, store.KeyFromNSN(types.NamespacedName{
					Name:      config.GetName(),
					Namespace: config.GetNamespace(),
				}), targetKey, tctx, config); err != nil {
					// This is bad since this means we cannot recover the applied config
					// on a target. We set the target config status to Failed.
					// Most likely a human intervention is needed
					cr.SetConditions(invv1alpha1.ConfigFailed(err.Error()))
					return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
				}
			}
			cr.SetConditions(invv1alpha1.ConfigReady())
			// apply the remaining config in async mode
			for _, config := range reApplyConfigs {
				if err := r.configProvider.Apply(ctx, store.KeyFromNSN(types.NamespacedName{
					Name:      config.GetName(),
					Namespace: config.GetNamespace(),
				}), targetKey, config, config); err != nil {
					log.Error("cannot apply config on target", "error", err)
				}
			}
		}

	} else {
		cr.SetConditions(invv1alpha1.ConfigFailed(string(configv1alpha1.ConditionReasonTargetNotReady)))
		condition := cr.GetCondition(invv1alpha1.ConditionTypeConfigReady)
		if condition.Status == metav1.ConditionTrue {
			configList, err := r.listTargetConfigs(ctx, cr)
			if err != nil {
				return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
			}
			for _, config := range configList.Items {
				log.Info("update config status", "config", config)
				condition := config.GetCondition(configv1alpha1.ConditionTypeReady)
				if condition.Status != metav1.ConditionFalse && condition.Message != string(configv1alpha1.ConditionReasonNotReady) {
					// update the status if not already set
					// resource version does not need to be updated, so we do a shortcut
					config.SetConditions(configv1alpha1.Failed(string(configv1alpha1.ConditionReasonNotReady)))
					if err := r.configProvider.UpdateStore(ctx, store.KeyFromNSN(types.NamespacedName{
						Name:      config.GetName(),
						Namespace: config.GetNamespace(),
					}), &config); err != nil {
						log.Error("cannot update config store", "error", err)
					}
				}
			}
		}
	}
	return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
}

func (r *reconciler) listTargetConfigs(ctx context.Context, cr *invv1alpha1.Target) (*configv1alpha1.ConfigList, error) {
	ctx = genericapirequest.WithNamespace(ctx, cr.GetNamespace())

	obj, err := r.configProvider.List(ctx, &internalversion.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("metadata.namespace", cr.GetNamespace()),
		LabelSelector: labels.SelectorFromSet(map[string]string{
			configv1alpha1.TargetNameKey: cr.GetName(),
		}),
	})
	if err != nil {
		return nil, err
	}
	configList, ok := obj.(*configv1alpha1.ConfigList)
	if !ok {
		return nil, fmt.Errorf("listTargetConfigs, unexpected object, wanted %s, got : %s",
			reflect.TypeOf(configv1alpha1.ConfigList{}).Name(),
			reflect.TypeOf(obj).Name(),
		)
	}
	return configList, nil
}

func (r *reconciler) GetTargetReadiness(ctx context.Context, key store.Key, cr *invv1alpha1.Target) (bool, *target.Context) {
	// we do not find the target Context -> target is not ready
	tctx, err := r.targetStore.Get(ctx, key)
	if err != nil {
		return false, nil
	}
	if cr.IsReady() && tctx.Client != nil && tctx.DataStore != nil && tctx.Ready {
		// target is trustable
		return true, &tctx
	}
	return false, &tctx
}

func (r *reconciler) getReApplyConfigs(ctx context.Context, cr *invv1alpha1.Target) ([]*configv1alpha1.Config, []*configv1alpha1.Config, error) {
	priorityReApplyConfigs := []*configv1alpha1.Config{}
	reApplyConfigs := []*configv1alpha1.Config{}
	configList, err := r.listTargetConfigs(ctx, cr)
	if err != nil {
		return nil, nil, err
	}
	for _, config := range configList.Items {
		if config.Status.AppliedConfig != nil {
			priorityReApplyConfigs = append(priorityReApplyConfigs, &config)
			// check if appliedConfig != desiredConfig; if so add this to the 2nd config group
			// to be reapplied
			appliedShaSum := configv1alpha1.GetShaSum(ctx, config.Status.AppliedConfig)
			desiredShaSum := configv1alpha1.GetShaSum(ctx, &config.Spec)
			if appliedShaSum != desiredShaSum {
				reApplyConfigs = append(reApplyConfigs, &config)
			}
		} else {
			reApplyConfigs = append(reApplyConfigs, &config)
		}
	}

	sort.Slice(priorityReApplyConfigs, func(i, j int) bool {
		return priorityReApplyConfigs[i].CreationTimestamp.Before(&priorityReApplyConfigs[j].CreationTimestamp)
	})
	sort.Slice(reApplyConfigs, func(i, j int) bool {
		return reApplyConfigs[i].CreationTimestamp.Before(&reApplyConfigs[j].CreationTimestamp)
	})

	return priorityReApplyConfigs, reApplyConfigs, err
}

func (r *reconciler) getLease(ctx context.Context, targetKey store.Key) lease.Lease {
	tctx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil {
		lease := lease.New(r.Client, targetKey.NamespacedName)
		r.targetStore.Create(ctx, targetKey, target.Context{Lease: lease})
		return lease
	}
	if tctx.Lease == nil {
		lease := lease.New(r.Client, targetKey.NamespacedName)
		tctx.Lease = lease
		r.targetStore.Update(ctx, targetKey, target.Context{Lease: lease})
		return lease
	}
	return tctx.Lease
}
