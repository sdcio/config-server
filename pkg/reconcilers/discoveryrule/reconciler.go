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

package discoveryrule

import (
	"context"
	"fmt"
	"reflect"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	memstore "github.com/henderiw/apiserver-store/pkg/storebackend/memory"
	"github.com/henderiw/logger/log"
	errors "github.com/pkg/errors"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/discovery/discoveryrule"
	invv1alpha1apply "github.com/sdcio/config-server/pkg/generated/applyconfiguration/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/eventhandler"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func init() {
	reconcilers.Register(crName, &reconciler{})
}

const (
	crName                = "discoveryrule"
	fieldmanagerfinalizer = "DiscoveryRuleController-finalizer"
	reconcilerName        = "DiscoveryRuleController"
	finalizer             = "discoveryrule.inv.sdcio.dev/finalizer"
	// errors
	errGetCr        = "cannot get cr"
	errUpdateStatus = "cannot update status"
)

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {
	_, ok := c.(*ctrlconfig.ControllerConfig)
	if !ok {
		return nil, fmt.Errorf("cannot initialize, expecting controllerConfig, got: %s", reflect.TypeOf(c).Name())
	}

	r.client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(
		mgr.GetClient(),
		finalizer,
		fieldmanagerfinalizer,
		func(name, namespace string, finalizers ...string) runtime.ApplyConfiguration {
			ac := invv1alpha1apply.DiscoveryRule(name, namespace)
			if len(finalizers) > 0 {
				ac.WithFinalizers(finalizers...)
			}
			return ac
		},
	)
	r.discoveryStore = memstore.NewStore[discoveryrule.DiscoveryRule]()
	r.recorder = mgr.GetEventRecorder(reconcilerName)
	r.baseCtx = ctx

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(reconcilerName).
		For(&invv1alpha1.DiscoveryRule{}).
		Watches(&invv1alpha1.TargetConnectionProfile{}, &eventhandler.TargetConnProfileForDiscoveryRuleEventHandler{Client: mgr.GetClient()}).
		Watches(&invv1alpha1.TargetSyncProfile{}, &eventhandler.TargetSyncProfileForDiscoveryRuleEventHandler{Client: mgr.GetClient()}).
		Watches(&corev1.Secret{}, &eventhandler.SecretForDiscoveryRuleEventHandler{Client: mgr.GetClient()}).
		Complete(r)
}

type reconciler struct {
	client    client.Client
	finalizer *resource.APIFinalizer

	discoveryStore storebackend.Storer[discoveryrule.DiscoveryRule]
	recorder       events.EventRecorder
	baseCtx        context.Context // manager-scoped, lives until shutdown
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, reconcilerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	key := storebackend.KeyFromNSN(req.NamespacedName)

	discoveryRule := &invv1alpha1.DiscoveryRule{}
	if err := r.client.Get(ctx, req.NamespacedName, discoveryRule); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if resource.IgnoreNotFound(err) != nil {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}

	discoveryRuleOrig := discoveryRule.DeepCopy()

	if !discoveryRule.GetDeletionTimestamp().IsZero() {
		// check if this is the last one -> if so stop the client to the dataserver
		dr, err := r.discoveryStore.Get(ctx, key)
		if err != nil {
			// discovery rule does not exist
			if err := r.finalizer.RemoveFinalizer(ctx, discoveryRule); err != nil {
				return ctrl.Result{Requeue: true},
					errors.Wrap(r.handleError(ctx, discoveryRuleOrig, "cannot delete finalizer", err), errUpdateStatus)
			}
			return ctrl.Result{}, nil
		}
		// stop and delete the discovery rule
		dr.Stop(r.baseCtx)
		if err := r.discoveryStore.Delete(ctx, key); err != nil {
			return ctrl.Result{Requeue: true},
				errors.Wrap(r.handleError(ctx, discoveryRuleOrig, "cannot delete discoveryRule from store", err), errUpdateStatus)
		}
		// remove the finalizer
		if err := r.finalizer.RemoveFinalizer(ctx, discoveryRule); err != nil {
			return ctrl.Result{Requeue: true},
				errors.Wrap(r.handleError(ctx, discoveryRuleOrig, "cannot delete finalizer", err), errUpdateStatus)
		}
		return ctrl.Result{}, nil
	}

	if err := discoveryRule.Validate(); err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, discoveryRuleOrig, "validation failed", err), errUpdateStatus)
	}

	if err := r.finalizer.AddFinalizer(ctx, discoveryRule); err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, discoveryRuleOrig, "cannot add finalizer", err), errUpdateStatus)
	}

	// check if the discovery rule is running
	isDRRunning := false
	dr, err := r.discoveryStore.Get(ctx, key)
	if err == nil {
		isDRRunning = true
	}

	// gather the references from the CR in a normalized format
	newDRConfig, err := r.getDRConfig(ctx, discoveryRule)
	if err != nil {
		//log.Error("cannot get discovery rule config", "error", err)
		if isDRRunning {
			// we stop the discovery rule
			dr.Stop(ctx)
			if err := r.discoveryStore.Delete(ctx, key); err != nil { // we don't fail
				log.Error("cannot delete discovery rule from store", "error", err)
			}
		}
		return ctrl.Result{}, // do no requeue
			errors.Wrap(r.handleError(ctx, discoveryRuleOrig, "cannot get normalized discoveryRule", err), errUpdateStatus)
	}

	if isDRRunning {
		currentDRConfig := dr.GetDiscoveryRulConfig()
		if !r.HasChanged(ctx, newDRConfig, currentDRConfig) {
			return ctrl.Result{}, errors.Wrap(r.handleSuccess(ctx, discoveryRuleOrig, false), errUpdateStatus)
		}
		// refs changed -> we stop the discovery rule
		dr.Stop(ctx)
		if err = r.discoveryStore.Delete(ctx, key); err != nil { // we dont fail
			log.Error("cannot delete discovery rule from store", "error", err)
		}
	}
	// create a new discoveryRule with the latest parameters
	dr = discoveryrule.New(r.client, newDRConfig)
	// new discovery initialization -> create or update (we deleted the DRConfig before)
	if err := r.discoveryStore.Create(ctx, key, dr); err != nil {
		// given this is a ummutable field this means the CR will have to be deleted/recreated
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, discoveryRuleOrig, "cannot get normalized discoveryRule", err), errUpdateStatus)
	}

	go func() {
		if err := dr.Run(r.baseCtx); err != nil {
			log.Error("run error", "err", err)
		}
	}()

	return ctrl.Result{}, errors.Wrap(r.handleSuccess(ctx, discoveryRuleOrig, true), errUpdateStatus)
}

func (r *reconciler) HasChanged(ctx context.Context, newDRConfig, currentDRConfig *discoveryrule.DiscoveryRuleConfig) bool {
	log := log.FromContext(ctx)

	// if the resource version changed the config has changed

	if newDRConfig.DiscoveryProfile != nil {
		log.Info("HasChanged",
			"CR Generation", fmt.Sprintf("%d/%d",
				newDRConfig.CR.GetGeneration(),
				currentDRConfig.CR.GetGeneration(),
			),
			"DiscoveryProfile Secret RV", fmt.Sprintf("%s/%s",
				newDRConfig.DiscoveryProfile.SecretResourceVersion,
				currentDRConfig.DiscoveryProfile.SecretResourceVersion,
			),
			"DiscoveryProfile Conn Profile len", fmt.Sprintf("%d/%d",
				len(newDRConfig.DiscoveryProfile.Connectionprofiles),
				len(currentDRConfig.DiscoveryProfile.Connectionprofiles),
			),
		)
	} else {
		log.Info("HasChanged",
			"CR Generation", fmt.Sprintf("%d/%d",
				newDRConfig.CR.GetGeneration(),
				currentDRConfig.CR.GetGeneration(),
			),
		)
	}

	if newDRConfig.CR.GetGeneration() != currentDRConfig.CR.GetGeneration() {
		return true
	}

	if newDRConfig.DiscoveryProfile != nil {
		// Validate Discovery profile
		if newDRConfig.DiscoveryProfile.SecretResourceVersion != currentDRConfig.DiscoveryProfile.SecretResourceVersion {
			return true
		}
		// check if a reference has changed
		if len(newDRConfig.DiscoveryProfile.Connectionprofiles) != len(currentDRConfig.DiscoveryProfile.Connectionprofiles) {
			return true
		}
		for idx, newConnProfile := range newDRConfig.DiscoveryProfile.Connectionprofiles {
			if newConnProfile.ResourceVersion != currentDRConfig.DiscoveryProfile.Connectionprofiles[idx].ResourceVersion {
				return true
			}
		}
	}

	if len(newDRConfig.TargetConnectionProfiles) != len(currentDRConfig.TargetConnectionProfiles) {
		return true
	}
	for i := range newDRConfig.TargetConnectionProfiles {
		// Validate Target Connetcion profiles profile
		if newDRConfig.TargetConnectionProfiles[i].SecretResourceVersion != currentDRConfig.TargetConnectionProfiles[i].SecretResourceVersion {
			return true
		}
		if newDRConfig.TargetConnectionProfiles[i].Connectionprofile.Generation != currentDRConfig.TargetConnectionProfiles[i].Connectionprofile.Generation {
			return true
		}
		if newDRConfig.TargetConnectionProfiles[i].Syncprofile.Generation != currentDRConfig.TargetConnectionProfiles[i].Syncprofile.Generation {
			return true
		}
	}

	return false
}

func (r *reconciler) handleSuccess(ctx context.Context, discoveryRule *invv1alpha1.DiscoveryRule, changed bool) error {
	log := log.FromContext(ctx)
	log.Debug("handleSuccess", "key", discoveryRule.GetNamespacedName(), "status old", discoveryRule.DeepCopy().Status)

	newCond := condv1alpha1.Ready()
	oldCond := discoveryRule.GetCondition(condv1alpha1.ConditionTypeReady)

	if newCond.Equal(oldCond) && !changed {
		log.Info("handleSuccess -> no change")
		return nil
	}

	statusApply := invv1alpha1apply.DiscoveryRuleStatus().
		WithConditions(newCond)

	if changed {
		statusApply = statusApply.WithStartTime(metav1.Now())
		r.recorder.Eventf(discoveryRule, nil, corev1.EventTypeNormal, invv1alpha1.DiscoveryRuleKind, "ready", "")
	}

	applyConfig := invv1alpha1apply.DiscoveryRule(discoveryRule.Name, discoveryRule.Namespace).
		WithStatus(statusApply)

	return r.client.Status().Apply(ctx, applyConfig, &client.SubResourceApplyOptions{
		ApplyOptions: client.ApplyOptions{
			FieldManager: reconcilerName,
		},
	})
}

func (r *reconciler) handleError(ctx context.Context, discoveryRule *invv1alpha1.DiscoveryRule, msg string, err error) error {
	log := log.FromContext(ctx)

	if err != nil {
		msg = fmt.Sprintf("%s err %s", msg, err.Error())
	}

	newCond := condv1alpha1.Failed(msg)
	oldCond := discoveryRule.GetCondition(condv1alpha1.ConditionTypeReady)

	// always update on error since StartTime changes
	_ = oldCond

	log.Error(msg)
	r.recorder.Eventf(discoveryRule, nil, corev1.EventTypeWarning, invv1alpha1.DiscoveryRuleKind, msg, "")

	applyConfig := invv1alpha1apply.DiscoveryRule(discoveryRule.Name, discoveryRule.Namespace).
		WithStatus(invv1alpha1apply.DiscoveryRuleStatus().
			WithConditions(newCond).
			WithStartTime(metav1.Now()),
		)

	return r.client.Status().Apply(ctx, applyConfig, &client.SubResourceApplyOptions{
		ApplyOptions: client.ApplyOptions{
			FieldManager: reconcilerName,
		},
	})
}
