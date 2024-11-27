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

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/pkg/errors"
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	"github.com/sdcio/config-server/pkg/target"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func init() {
	reconcilers.Register(crName, &reconciler{})
}

const (
	crName         = "targetconfigserver"
	reconcilerName = "TargetConfigServerController"
	finalizer      = "targetconfigserver.inv.sdcio.dev/finalizer"
	// errors
	errGetCr           = "cannot get cr"
	errUpdateDataStore = "cannot update datastore"
	errUpdateStatus    = "cannot update status"
)

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {
	cfg, ok := c.(*ctrlconfig.ControllerConfig)
	if !ok {
		return nil, fmt.Errorf("cannot initialize, expecting controllerConfig, got: %s", reflect.TypeOf(c).Name())
	}

	r.Client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer, reconcilerName)
	r.targetStore = cfg.TargetStore
	r.recorder = mgr.GetEventRecorderFor(reconcilerName)

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(reconcilerName).
		For(&invv1alpha1.Target{}).
		Complete(r)
}

type reconciler struct {
	client.Client
	finalizer   *resource.APIFinalizer
	targetStore storebackend.Storer[*target.Context]
	recorder    record.EventRecorder
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, reconcilerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	targetKey := storebackend.KeyFromNSN(req.NamespacedName)

	target := &invv1alpha1.Target{}
	if err := r.Get(ctx, req.NamespacedName, target); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if resource.IgnoreNotFound(err) != nil {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}

	targetOrig := target.DeepCopy()
	if !target.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, nil
	}

	if target.Status.GetCondition(invv1alpha1.ConditionTypeDatastoreReady).Status != metav1.ConditionTrue {
		// datastore not ready so we can wait till the target goes to ready state
		return ctrl.Result{}, // requeue will happen automatically when discovery is done
			errors.Wrap(r.handleError(ctx, targetOrig, "target datastore not ready", nil), errUpdateStatus)
	}

	ready, tctx := r.GetTargetReadiness(ctx, targetKey, target)
	if !ready {
		return ctrl.Result{}, errors.Wrap(r.handleError(ctx, targetOrig, string(configv1alpha1.ConditionReasonTargetNotReady), nil), errUpdateStatus)
	}

	cfgCondition := target.GetCondition(invv1alpha1.ConditionTypeConfigReady)
	if cfgCondition.Status == metav1.ConditionFalse &&
		cfgCondition.Reason != string(invv1alpha1.ConditionReasonReApplyFailed) {

		//log.Info("target reapply config")
		// we split the config in config that were successfully applied and config that was not yet
		reApplyConfigs, err := r.getReApplyConfigs(ctx, target)
		if err != nil {
			return ctrl.Result{}, errors.Wrap(r.handleError(ctx, targetOrig, "reapply config failed", err), errUpdateStatus)
		}

		// We need to restore the config on the target
		for _, config := range reApplyConfigs {
			if _, err := tctx.SetIntent(ctx, targetKey, config, false, false); err != nil {
				// This is bad since this means we cannot recover the applied config
				// on a target. We set the target config status to Failed.
				// Most likely a human intervention is needed
				return ctrl.Result{}, errors.Wrap(r.handleError(ctx, targetOrig, "setIntent failed", err), errUpdateStatus)
			}
		}

		return ctrl.Result{}, errors.Wrap(r.handleSuccess(ctx, targetOrig), errUpdateStatus)
	}

	return ctrl.Result{}, nil
}

func (r *reconciler) handleSuccess(ctx context.Context, target *invv1alpha1.Target) error {
	log := log.FromContext(ctx)
	log.Debug("handleSuccess", "key", target.GetNamespacedName(), "status old", target.DeepCopy().Status)
	// take a snapshot of the current object
	patch := client.MergeFrom(target.DeepCopy())
	// update status
	target.SetConditions(invv1alpha1.ConfigReady())
	//target.SetOverallStatus()
	r.recorder.Eventf(target, corev1.EventTypeNormal, invv1alpha1.TargetKind, "config ready")

	log.Debug("handleSuccess", "key", target.GetNamespacedName(), "status new", target.Status)

	return r.Client.Status().Patch(ctx, target, patch, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
		},
	})
}

func (r *reconciler) handleError(ctx context.Context, target *invv1alpha1.Target, msg string, err error) error {
	log := log.FromContext(ctx)
	// take a snapshot of the current object
	patch := client.MergeFrom(target.DeepCopy())

	if err != nil {
		msg = fmt.Sprintf("%s err %s", msg, err.Error())
	}
	target.SetConditions(invv1alpha1.ConfigFailed(msg))
	//target.SetOverallStatus()
	log.Error(msg, "error", err)
	r.recorder.Eventf(target, corev1.EventTypeWarning, invv1alpha1.TargetKind, msg)

	return r.Client.Status().Patch(ctx, target, patch, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
		},
	})
}

func (r *reconciler) listTargetConfigs(ctx context.Context, target *invv1alpha1.Target) (*config.ConfigList, error) {
	ctx = genericapirequest.WithNamespace(ctx, target.GetNamespace())

	opts := []client.ListOption{
		client.MatchingLabels{
			config.TargetNamespaceKey: target.GetNamespace(),
			config.TargetNameKey:      target.GetName(),
		},
	}

	v1alpha1configList := &configv1alpha1.ConfigList{}
	if err := r.List(ctx, v1alpha1configList, opts...); err != nil {
		return nil, err
	}
	configList := &config.ConfigList{}
	configv1alpha1.Convert_v1alpha1_ConfigList_To_config_ConfigList(v1alpha1configList, configList, nil)

	return configList, nil
}

func (r *reconciler) GetTargetReadiness(ctx context.Context, key storebackend.Key, target *invv1alpha1.Target) (bool, *target.Context) {
	// we do not find the target Context -> target is not ready
	tctx, err := r.targetStore.Get(ctx, key)
	if err != nil {
		return false, nil
	}
	if target.IsDatastoreReady() && tctx.IsReady() {
		return true, tctx
	}
	return false, tctx
}

func (r *reconciler) getReApplyConfigs(ctx context.Context, target *invv1alpha1.Target) ([]*config.Config, error) {
	configs := []*config.Config{}
	configList, err := r.listTargetConfigs(ctx, target)
	if err != nil {
		return nil, err
	}
	for _, config := range configList.Items {
		if config.Status.AppliedConfig != nil {
			configs = append(configs, &config)
		}
	}

	sort.Slice(configs, func(i, j int) bool {
		return configs[i].CreationTimestamp.Before(&configs[j].CreationTimestamp)
	})

	return configs, err
}
