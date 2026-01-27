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
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	targetmanager "github.com/sdcio/config-server/pkg/sdc/target/manager"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	crName                = "targetrecoveryserver"
	fieldmanagerfinalizer = "targetrecoveryfinalizerr"
	reconcilerName        = "TargetRecoveryServerController"
	finalizer             = "targetrecoveryserver.inv.sdcio.dev/finalizer"
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

	if cfg.TargetManager == nil {
		return nil, fmt.Errorf("TargetManager is nil: set LOCAL_DATASERVER=true or disable TargetRecoveryServerController")
	}

	var err error
	r.discoveryClient, err = ctrlconfig.GetDiscoveryClient(mgr)
	if err != nil {
		return nil, fmt.Errorf("cannot get discoveryClient from manager")
	}

	r.client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer, reconcilerName)
	r.targetMgr = cfg.TargetManager
	r.recorder = mgr.GetEventRecorderFor(reconcilerName)
	r.transactor = targetmanager.NewTransactor(r.client, crName, fieldmanagerfinalizer)

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(reconcilerName).
		For(&invv1alpha1.Target{}).
		Complete(r)
}

type reconciler struct {
	client          client.Client
	discoveryClient *discovery.DiscoveryClient
	finalizer       *resource.APIFinalizer
	targetMgr       *targetmanager.TargetManager
	recorder        record.EventRecorder
	transactor      *targetmanager.Transactor
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

	dsctx, ok := r.targetMgr.GetDatastore(ctx, targetKey)
	if !ok || dsctx == nil {
		// not ready -> requeue, or set condition
		return ctrl.Result{RequeueAfter: 5 * time.Second},
			errors.Wrap(r.handleError(ctx, targetOrig,
				"target runtime not ready (no dsctx yet)",
				nil),
				errUpdateStatus)
	}

	if dsctx.Client == nil {
		return ctrl.Result{RequeueAfter: 5 * time.Second},
			errors.Wrap(r.handleError(ctx, targetOrig,
				fmt.Sprintf("target runtime not ready phase=%s dsReady=%t dsStoreReady=%t recovered=%t err=%v",
					dsctx.Status.Phase, dsctx.Status.DSReady, dsctx.Status.DSStoreReady, dsctx.Status.Recovered, dsctx.Status.LastError),
				nil),
				errUpdateStatus)
	}

	if dsctx.Status.Recovered {
		log.Info("config recovery -> already recovered")
		return ctrl.Result{}, errors.Wrap(r.handleSuccess(ctx, targetOrig, nil), errUpdateStatus)
	}

	msg, err := r.transactor.RecoverConfigs(ctx, target, dsctx)
	if err != nil {
		// This is bad since this means we cannot recover the applied config
		// on a target. We set the target config status to Failed.
		// Most likely a human intervention is needed
		return ctrl.Result{}, errors.Wrap(r.handleError(ctx, targetOrig, "setIntent failed", err), errUpdateStatus)
	}

	return ctrl.Result{}, errors.Wrap(r.handleSuccess(ctx, targetOrig, msg), errUpdateStatus)
}

func (r *reconciler) handleSuccess(ctx context.Context, target *invv1alpha1.Target, msg *string) error {
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
	// set new conditions
	newMsg := ""
	if msg != nil {
		newMsg = *msg
	}
	newTarget.SetConditions(invv1alpha1.ConfigReady(newMsg))

	oldCond := target.GetCondition(invv1alpha1.ConditionTypeConfigReady)
	newCond := newTarget.GetCondition(invv1alpha1.ConditionTypeConfigReady)

	changed := !newCond.Equal(oldCond)

	// we don't update the resource if no condition changed
	if !changed {
		// we don't update the resource if no condition changed
		log.Info("handleSuccess -> no change")
		return nil
	}
	log.Info("handleSuccess -> change",
		"condition change", newTarget.GetCondition(invv1alpha1.ConditionTypeConfigReady).Equal(target.GetCondition(invv1alpha1.ConditionTypeConfigReady)))

	r.recorder.Eventf(newTarget, corev1.EventTypeNormal, invv1alpha1.TargetKind, "config ready")

	return r.client.Status().Patch(ctx, newTarget, client.Apply, &client.SubResourcePatchOptions{
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
	// set new conditions
	newTarget.SetConditions(invv1alpha1.ConfigFailed(msg))

	log.Warn(msg, "error", err)
	oldCond := target.GetCondition(invv1alpha1.ConditionTypeConfigReady)
	newCond := newTarget.GetCondition(invv1alpha1.ConditionTypeConfigReady)

	changed := !newCond.Equal(oldCond)

	if !changed {
		// we don't update the resource if no condition changed
		log.Info("handleError -> no change")
		return nil
	}

	r.recorder.Eventf(newTarget, corev1.EventTypeWarning, invv1alpha1.TargetKind, msg)
	log.Info("handleError -> change",
		"condition change", newTarget.GetCondition(invv1alpha1.ConditionTypeConfigReady).Equal(target.GetCondition(invv1alpha1.ConditionTypeConfigReady)))

	return r.client.Status().Patch(ctx, newTarget, client.Apply, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
		},
	})
}
