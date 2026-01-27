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

package target

/*

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	pkgerrors "github.com/pkg/errors"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	sdctarget "github.com/sdcio/config-server/pkg/sdc/target"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
)

func init() {
	reconcilers.Register(crName, &reconciler{})
}

const (
	crName         = "target"
	reconcilerName = "TargetController"
	finalizer      = "target.inv.sdcio.dev/finalizer"
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

	r.client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer, reconcilerName)
	r.targetStore = cfg.TargetStore
	r.recorder = mgr.GetEventRecorderFor(reconcilerName)

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(reconcilerName).
		For(&invv1alpha1.Target{}).
		Complete(r)
}

type reconciler struct {
	client client.Client
	finalizer       *resource.APIFinalizer
	targetStore     storebackend.Storer[*sdctarget.Context]
	recorder        record.EventRecorder
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, reconcilerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	targetKey := storebackend.KeyFromNSN(req.NamespacedName)

	target := &invv1alpha1.Target{}
	if err := r.client.Get(ctx, req.NamespacedName, target); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if resource.IgnoreNotFound(err) != nil {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, pkgerrors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}

	targetOrig := target.DeepCopy()

	if !target.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, nil
	}

	if target.Status.GetCondition(invv1alpha1.ConditionTypeDatastoreReady).Status != metav1.ConditionTrue {
		// target not ready so we can wait till the target goes to ready state
		return ctrl.Result{},
			pkgerrors.Wrap(r.handleError(ctx, targetOrig, "datastore not ready", nil), errUpdateStatus)
	}

	tctx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil {
		return ctrl.Result{Requeue: true, RequeueAfter: 1 * time.Second},
			pkgerrors.Wrap(r.handleError(ctx, targetOrig, "tcxt does not exist", err), errUpdateStatus)
	}

	resp, err := tctx.GetDataStore(ctx, &sdcpb.GetDataStoreRequest{DatastoreName: targetKey.String()})
	if err != nil {
		if errs := r.targetStore.UpdateWithKeyFn(ctx, targetKey, func(ctx context.Context, tctx *sdctarget.Context) *sdctarget.Context {
			if tctx != nil {
				tctx.SetNotReady(ctx)
			}
			return tctx
		}); errs != nil {
			errs = errors.Join(errs, err)
			return ctrl.Result{Requeue: true},
				pkgerrors.Wrap(r.handleError(ctx, targetOrig, "target datastore rsp error and update targetstore failed", errs), errUpdateStatus)
		}
		return ctrl.Result{RequeueAfter: 5 * time.Second},
			pkgerrors.Wrap(r.handleError(ctx, targetOrig, "target datastore rsp error", err), errUpdateStatus)
	}
	if resp.Target.Status != sdcpb.TargetStatus_CONNECTED {
		if errs := r.targetStore.UpdateWithKeyFn(ctx, targetKey, func(ctx context.Context, tctx *sdctarget.Context) *sdctarget.Context {
			if tctx != nil {
				tctx.SetNotReady(ctx)
			}
			return tctx
		}); errs != nil {
			errs = errors.Join(errs, err)
			return ctrl.Result{Requeue: true},
				pkgerrors.Wrap(r.handleError(ctx, targetOrig, "target datastore not connected and update targetstore failed", errs), errUpdateStatus)
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, // requeue will happen automatically when target gets updated
			pkgerrors.Wrap(r.handleError(ctx, targetOrig, "target datastore not connected", err), errUpdateStatus)
	}

	if errs := r.targetStore.UpdateWithKeyFn(ctx, targetKey, func(ctx context.Context, tctx *sdctarget.Context) *sdctarget.Context {
		if tctx != nil {
			tctx.SetReady(ctx)
		}
		return tctx
	}); errs != nil {
		errs = errors.Join(errs, err)
		return ctrl.Result{Requeue: true},
			pkgerrors.Wrap(r.handleError(ctx, targetOrig, "update targetstore failed", errs), errUpdateStatus)
	}


	return r.handleSuccess(ctx, targetOrig)
}

func (r *reconciler) handleSuccess(ctx context.Context, target *invv1alpha1.Target) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Debug("handleSuccess", "key", target.GetNamespacedName(), "status old", target.DeepCopy().Status)
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
	newTarget.SetConditions(target.GetCondition(invv1alpha1.ConditionTypeTargetConnectionReady))
	newTarget.SetConditions(target.GetCondition(condv1alpha1.ConditionTypeReady))
	// set new conditions
	newTarget.SetConditions(invv1alpha1.TargetConnectionReady())
	newTarget.SetOverallStatus(target)


	result := ctrl.Result{RequeueAfter: 5 * time.Minute}
	if !target.IsReady() {
		result= ctrl.Result{Requeue: true}
	}
	
	// we don't update the resource if no condition changed
	if newTarget.GetCondition(invv1alpha1.ConditionTypeTargetConnectionReady).Equal(target.GetCondition(invv1alpha1.ConditionTypeTargetConnectionReady)) &&
		newTarget.GetCondition(condv1alpha1.ConditionTypeReady).Equal(target.GetCondition(condv1alpha1.ConditionTypeReady)) {
			log.Info("handleSuccess -> no change")
			return result, nil
	}
	log.Info("handleSuccess -> change", 
			"connReady condition change", newTarget.GetCondition(invv1alpha1.ConditionTypeTargetConnectionReady).Equal(target.GetCondition(invv1alpha1.ConditionTypeTargetConnectionReady)),
			"Ready condition change", newTarget.GetCondition(condv1alpha1.ConditionTypeReady).Equal(target.GetCondition(condv1alpha1.ConditionTypeReady)),
	)

	r.recorder.Eventf(newTarget, corev1.EventTypeNormal, invv1alpha1.TargetKind, "ready")

	log.Debug("handleSuccess", "key", newTarget.GetNamespacedName(), "status new", target.Status)

	return result, r.client.Status().Patch(ctx, newTarget, client.Apply, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
		},
	})
}

func (r *reconciler) handleError(ctx context.Context, target *invv1alpha1.Target, msg string, err error) error {
	log := log.FromContext(ctx)
	// take a snapshot of the current object
	//patch := client.MergeFrom(target.DeepCopy())

	if err != nil {
		msg = fmt.Sprintf("%s err %s", msg, err.Error())
	}

	newTarget := invv1alpha1.BuildTarget(
		metav1.ObjectMeta{
			Namespace: target.Namespace,
			Name:      target.Name,
		},
		invv1alpha1.TargetSpec{},
		invv1alpha1.TargetStatus{},
	)
	// set old condition to avoid updating the new status if not changed
	newTarget.SetConditions(target.GetCondition(invv1alpha1.ConditionTypeTargetConnectionReady))
	newTarget.SetConditions(target.GetCondition(condv1alpha1.ConditionTypeReady))
	// set new conditions
	newTarget.SetConditions(invv1alpha1.TargetConnectionFailed(msg))
	newTarget.SetOverallStatus(target)

	if newTarget.GetCondition(invv1alpha1.ConditionTypeTargetConnectionReady).Equal(target.GetCondition(invv1alpha1.ConditionTypeTargetConnectionReady)) &&
		newTarget.GetCondition(condv1alpha1.ConditionTypeReady).Equal(target.GetCondition(condv1alpha1.ConditionTypeReady)) {
			log.Info("handleSuccess -> no change")
			return nil
	}
	log.Info("handleSuccess -> change", 
			"connReady condition change", newTarget.GetCondition(invv1alpha1.ConditionTypeTargetConnectionReady).Equal(target.GetCondition(invv1alpha1.ConditionTypeTargetConnectionReady)),
			"Ready condition change", newTarget.GetCondition(condv1alpha1.ConditionTypeReady).Equal(target.GetCondition(condv1alpha1.ConditionTypeReady)),
	)

	log.Error(msg, "error", err)
	r.recorder.Eventf(newTarget, corev1.EventTypeWarning, invv1alpha1.TargetKind, msg)

	return r.client.Status().Patch(ctx, newTarget, client.Apply, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
		},
	})
}
*/



