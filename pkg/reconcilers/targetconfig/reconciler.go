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
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/pkg/errors"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/eventhandler"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	"github.com/sdcio/config-server/pkg/target"
	"github.com/sdcio/config-server/pkg/transactor"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func init() {
	reconcilers.Register(crName, &reconciler{})
}

const (
	crName                = "targetconfigserver"
	fieldmanagerfinalizer = "targetconfigfinalizerr"
	reconcilerName        = "TargetConfigServerController"
	finalizer             = "targetconfigserver.inv.sdcio.dev/finalizer"
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
	var err error
	r.discoveryClient, err = ctrlconfig.GetDiscoveryClient(mgr)
	if err != nil {
		return nil, fmt.Errorf("cannot get discoveryClient from manager")
	}

	r.client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer, reconcilerName)
	r.targetStore = cfg.TargetStore
	r.recorder = mgr.GetEventRecorderFor(reconcilerName)
	r.transactor = transactor.New(r.client, crName, fieldmanagerfinalizer)

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(reconcilerName).
		For(&invv1alpha1.Target{}).
		Watches(&configv1alpha1.Config{}, &eventhandler.ConfigForTargetEventHandler{Client: mgr.GetClient(), ControllerName: reconcilerName}).
		Watches(&configv1alpha1.Deviation{}, &eventhandler.DeviationForTargetEventHandler{Client: mgr.GetClient(), ControllerName: reconcilerName}).
		Complete(r)
}

type reconciler struct {
	client client.Client
	discoveryClient *discovery.DiscoveryClient
	finalizer       *resource.APIFinalizer
	targetStore     storebackend.Storer[*target.Context]
	recorder        record.EventRecorder
	transactor      *transactor.Transactor
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, reconcilerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	if _, err := r.discoveryClient.ServerResourcesForGroupVersion(configv1alpha1.SchemeGroupVersion.String()); err != nil {
		log.Info("API group not available, retrying...", "groupversion", configv1alpha1.SchemeGroupVersion.String())
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	targetKey := storebackend.KeyFromNSN(req.NamespacedName)

	target := &invv1alpha1.Target{}
	if err := r.client.Get(ctx, req.NamespacedName, target); err != nil {
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

	// to reapply existing configs we should check if the datastore is ready and if the target context is ready
	// DataStore ready means: target is discovered, datastore is created and connection to the dataserver is up + target context is ready
	tctx, err := r.IsTargetDataStoreReady(ctx, targetKey, target)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(r.handleError(ctx, targetOrig, err), errUpdateStatus)
	}

	// if the config is not receovered we stop the reconcile loop
	if !tctx.IsTargetConfigRecovered(ctx) {
		log.Info("config recovery -> target config not recovered")
		return ctrl.Result{}, nil
	}

	retry, err := r.transactor.Transact(ctx, target, tctx)
	if err != nil {
		log.Error("config transaction failed", "retry", retry, "err", err)
		// This is bad since this means we cannot recover the applied config
		// on a target. We set the target config status to Failed.
		// Most likely a human intervention is needed
		if retry {
			return ctrl.Result{
				RequeueAfter: 500 * time.Millisecond,
				Requeue: true,
			}, 
			errors.Wrap(r.handleError(ctx, targetOrig, err), errUpdateStatus)	
		}
		return ctrl.Result{}, errors.Wrap(r.handleError(ctx, targetOrig, err), errUpdateStatus)
	}
	log.Info("config transaction success", "retry", retry)
	if retry {
			return ctrl.Result{
				RequeueAfter: 500 * time.Millisecond,
				Requeue: true,
			}, 
			errors.Wrap(r.handleSuccess(ctx, targetOrig), errUpdateStatus)
		}
	return ctrl.Result{}, errors.Wrap(r.handleSuccess(ctx, targetOrig), errUpdateStatus)
}

func (r *reconciler) handleSuccess(ctx context.Context, target *invv1alpha1.Target) error {
	log := log.FromContext(ctx)
	log.Debug("handleSuccess", "key", target.GetNamespacedName(), "status old", target.DeepCopy().Status)
	return nil
	/*
	// take a snapshot of the current object
	//patch := client.MergeFrom(target.DeepCopy())
	// update status
	newTarget := invv1alpha1.BuildTarget(
		metav1.ObjectMeta{
			Namespace: target.Namespace,
			Name:      target.Name,
		},
		invv1alpha1.TargetSpec{},
		invv1alpha1.TargetStatus{},
	)
	// set old condition to avoid updating the new status if not changed
	newTarget.SetConditions(target.GetCondition(invv1alpha1.ConditionTypeConfigReady))
	// set new conditions
	newTarget.SetConditions(invv1alpha1.ConfigReady(msg))

	log.Debug("handleSuccess", "key", newTarget.GetNamespacedName(), "status new", target.Status)

	// we don't update the resource if no condition changed
	if newTarget.GetCondition(invv1alpha1.ConditionTypeConfigReady).Equal(target.GetCondition(invv1alpha1.ConditionTypeConfigReady)) {
		// we don't update the resource if no condition changed
		log.Info("handleSuccess -> no change")
		return nil
	}
	log.Info("handleSuccess -> change",
		"condition change", newTarget.GetCondition(invv1alpha1.ConditionTypeConfigReady).Equal(target.GetCondition(invv1alpha1.ConditionTypeConfigReady)))

	r.recorder.Eventf(newTarget, corev1.EventTypeNormal, invv1alpha1.TargetKind, "config ready")

	return r.Client.Status().Patch(ctx, newTarget, client.Apply, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
		},
	})
		*/
}

func (r *reconciler) handleError(ctx context.Context, target *invv1alpha1.Target, err error) error {
	log := log.FromContext(ctx)
	log.Error("config transaction failed", "err", err)
	return nil
	// take a snapshot of the current object
	//patch := client.MergeFrom(target.DeepCopy())

	
	/*
	newTarget := invv1alpha1.BuildTarget(
		metav1.ObjectMeta{
			Namespace: target.Namespace,
			Name:      target.Name,
		},
		invv1alpha1.TargetSpec{},
		invv1alpha1.TargetStatus{},
	)
	// set old condition to avoid updating the new status if not changed
	newTarget.SetConditions(target.GetCondition(invv1alpha1.ConditionTypeConfigReady))
	// set new conditions
	newTarget.SetConditions(invv1alpha1.ConfigFailed(msg))
	//target.SetOverallStatus()
	log.Error(msg, "error", err)
	r.recorder.Eventf(newTarget, corev1.EventTypeWarning, invv1alpha1.TargetKind, msg)

	if newTarget.GetCondition(invv1alpha1.ConditionTypeConfigReady).Equal(target.GetCondition(invv1alpha1.ConditionTypeConfigReady)) {
		// we don't update the resource if no condition changed
		log.Info("handleError -> no change")
		return nil
	}
	log.Info("handleError -> change",
		"condition change", newTarget.GetCondition(invv1alpha1.ConditionTypeConfigReady).Equal(target.GetCondition(invv1alpha1.ConditionTypeConfigReady)))

	return r.Client.Status().Patch(ctx, newTarget, client.Apply, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
		},
	})
		*/
}

func (r *reconciler) IsTargetDataStoreReady(ctx context.Context, key storebackend.Key, target *invv1alpha1.Target) (*target.Context, error) {
	log := log.FromContext(ctx)
	// we do not find the target Context -> target is not ready
	tctx, err := r.targetStore.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("no target context")
	}
	log.Info("getTargetReadiness", "datastoreReady", target.IsDatastoreReady(), "tctx ready", tctx.IsReady())
	if !target.IsDatastoreReady() || !tctx.IsReady() {
		return tctx, fmt.Errorf("target not ready")
	}
	return tctx, nil
}