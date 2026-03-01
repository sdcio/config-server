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
	configv1alpha1apply "github.com/sdcio/config-server/pkg/generated/applyconfiguration/config/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	targetmanager "github.com/sdcio/config-server/pkg/sdc/target/manager"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func init() {
	reconcilers.Register(crName, &reconciler{})
}

const (
	crName                = "targetrecoveryconfig"
	fieldmanagerfinalizer = "targetrecoveryconfigfinalizer"
	reconcilerName        = "TargetRecoveryConfigController"
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
	r.recorder = mgr.GetEventRecorder(reconcilerName)
	r.transactor = targetmanager.NewTransactor(r.client, "transactor")

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(reconcilerName).
		For(&configv1alpha1.Target{}).
		Complete(r)
}

type reconciler struct {
	client          client.Client
	discoveryClient *discovery.DiscoveryClient
	finalizer       *resource.APIFinalizer
	targetMgr       *targetmanager.TargetManager
	recorder        events.EventRecorder
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

	target := &configv1alpha1.Target{}
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

func (r *reconciler) handleSuccess(ctx context.Context, target *configv1alpha1.Target, msg *string) error {
	log := log.FromContext(ctx)
	newMsg := ""
	if msg != nil {
		newMsg = *msg
	}
	newCond := configv1alpha1.TargetConfigRecoveryReady(newMsg)
	oldCond := target.GetCondition(configv1alpha1.ConditionTypeTargetConfigRecoveryReady)

	if newCond.Equal(oldCond) {
		log.Info("handleSuccess -> no change")
		return nil
	}

	log.Info("handleSuccess -> change", "condition change", true)
	r.recorder.Eventf(target, nil, corev1.EventTypeNormal, configv1alpha1.TargetKind, "config recovery ready", "")

	applyConfig := configv1alpha1apply.Target(target.Name, target.Namespace).
		WithStatus(configv1alpha1apply.TargetStatus().
			WithConditions(newCond),
		)

	return r.client.Status().Apply(ctx, applyConfig, &client.SubResourceApplyOptions{
		ApplyOptions: client.ApplyOptions{
			FieldManager: reconcilerName,
			Force:        ptr.To(true),
		},
	})
}

func (r *reconciler) handleError(ctx context.Context, target *configv1alpha1.Target, msg string, err error) error {
	log := log.FromContext(ctx)

	if err != nil {
		msg = fmt.Sprintf("%s err %s", msg, err.Error())
	}
	newCond := configv1alpha1.TargetConfigRecoveryFailed(msg)
	oldCond := target.GetCondition(configv1alpha1.ConditionTypeTargetConfigRecoveryReady)

	if newCond.Equal(oldCond) {
		log.Info("handleError -> no change")
		return nil
	}

	log.Warn(msg, "error", err)
	log.Info("handleError -> change", "condition change", true)
	r.recorder.Eventf(target, nil, corev1.EventTypeWarning, configv1alpha1.TargetKind, msg, "")

	applyConfig := configv1alpha1apply.Target(target.Name, target.Namespace).
		WithStatus(configv1alpha1apply.TargetStatus().
			WithConditions(newCond),
		)

	return r.client.Status().Apply(ctx, applyConfig, &client.SubResourceApplyOptions{
		ApplyOptions: client.ApplyOptions{
			FieldManager: reconcilerName,
			Force:        ptr.To(true),
		},
	})
}
