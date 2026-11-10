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
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	configv1alpha1apply "github.com/sdcio/config-server/pkg/generated/applyconfiguration/config/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/eventhandler"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	targetmanager "github.com/sdcio/config-server/pkg/sdc/target/manager"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func init() {
	reconcilers.Register(crName, &reconciler{})
}

const (
	crName                = "targetconfig"
	fieldmanagerfinalizer = "TargetConfigController-finalizer"
	reconcilerName        = "TargetConfigController"
	finalizer             = "targetconfig.inv.sdcio.dev/finalizer"
	// errors
	errGetCr           = "cannot get cr"
	errUpdateDataStore = "cannot update datastore"
	errUpdateStatus    = "cannot update status"

	// runtimeRequeue polls the target runtime, its transitions are not watched
	runtimeRequeue = 5 * time.Second
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

	if cfg.TargetManager == nil {
		return nil, fmt.Errorf("TargetManager is nil: set LOCAL_DATASERVER=true or disable TargetConfigServerController")
	}

	r.client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(
		mgr.GetClient(),
		finalizer,
		fieldmanagerfinalizer,
		func(name, namespace string, finalizers ...string) runtime.ApplyConfiguration {
			ac := configv1alpha1apply.Target(name, namespace)
			if len(finalizers) > 0 {
				ac.WithFinalizers(finalizers...)
			}
			return ac
		},
	)
	r.targetMgr = cfg.TargetManager
	r.recorder = mgr.GetEventRecorder(reconcilerName)
	r.transactor = targetmanager.NewTransactor(r.client, "transactor")

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(reconcilerName).
		WithOptions(controller.Options{
			// Recoverable failures are returned as errors;
			// Use custom rate limiter to cap the backoff max to 60s
			// instead of the default ~1000s cap.
			RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](
				1*time.Second, 60*time.Second),
		}).
		For(&configv1alpha1.Target{}).
		// Config events are mapped onto the Target named in the Config's labels.
		// This controller is therefore the only one that sees a Config whose
		// Target does not exist, and must report it (sdcio/config-server#469).
		Watches(&configv1alpha1.Config{}, &eventhandler.ConfigForTargetEventHandler{Client: mgr.GetClient(), ControllerName: reconcilerName}).
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
		if !apierrors.IsNotFound(err) {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, errors.Wrap(err, errGetCr)
		}
		// The Target does not exist: never created, or already deleted.
		// Configs referencing it are only reconciled through this controller,
		// so they would otherwise keep an empty (or stale) status.
		return r.reconcileTargetNotFound(ctx, req.NamespacedName)
	}

	if !target.GetDeletionTimestamp().IsZero() {
		if err := r.setConfigsNotReady(ctx, target,
			configv1alpha1.ConfigReasonTargetDeleting, "target is being deleted",
		); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.finalizer.RemoveFinalizer(ctx, target); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "cannot remove finalizer")
		}
		log.Debug("Successfully deleted resource")
		return ctrl.Result{}, nil
	}

	if err := r.finalizer.AddFinalizer(ctx, target); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "cannot add finalizer")
	}

	// --- the first target stage that blocks sets the Config Ready reason ---

	// 1) Target Ready, owned by the target datastore controller. Its reason
	// (the blocking target pipeline stage) goes into the message.
	// No requeue: Target status changes trigger this controller (For(&Target{})).
	if !target.IsReady() {
		return ctrl.Result{}, r.setConfigsNotReady(ctx, target,
			configv1alpha1.ConfigReasonTargetNotReady, target.NotReadyReason())
	}

	// 2) runtime: Target Ready can lag the runtime (or the runtime moved on
	// since), and runtime transitions do not trigger this controller -> requeue.
	dsctx, ok := r.targetMgr.GetDatastore(ctx, targetKey)
	if !ok || dsctx == nil {
		msg := "target runtime not ready"
		if dsctx != nil {
			msg = fmt.Sprintf("target runtime not ready: phase=%s dsReady=%t dsStoreReady=%t recovered=%t",
				dsctx.Status.Phase, dsctx.Status.DSReady, dsctx.Status.DSStoreReady, dsctx.Status.Recovered)
			if dsctx.Status.LastError != "" {
				msg += ": " + dsctx.Status.LastError
			}
		}
		return ctrl.Result{RequeueAfter: runtimeRequeue}, r.setConfigsNotReady(ctx, target,
			configv1alpha1.ConfigReasonTargetRuntimeNotReady, msg)
	}

	// 3) transact: also sets Ready on the configs of the target
	retry, err := r.transactor.Transact(ctx, target, dsctx)
	if err != nil {
		log.Warn("config transaction failed", "retry", retry, "err", err)
		if !retry {
			// non recoverable: the failure is reported on the Configs by the
			// transaction; retrying does not help.
			return ctrl.Result{}, nil
		}
		// recoverable: back off and retry
		return ctrl.Result{}, err
	}
	log.Info("config transaction success", "retry", retry)

	if retry {
		return ctrl.Result{RequeueAfter: 500 * time.Millisecond}, nil
	}
	return ctrl.Result{}, nil
}

// reconcileTargetNotFound marks every Config that references the missing
// Target. No requeue: creating the Target triggers this controller through
// For(&Target{}), and the normal path takes over.
func (r *reconciler) reconcileTargetNotFound(ctx context.Context, key types.NamespacedName) (ctrl.Result, error) {
	// The Configs are selected by the target name/namespace, so a stub
	// carrying only those is sufficient.
	missing := &configv1alpha1.Target{ObjectMeta: metav1.ObjectMeta{
		Namespace: key.Namespace,
		Name:      key.Name,
	}}
	return ctrl.Result{}, r.setConfigsNotReady(ctx, missing,
		configv1alpha1.ConfigReasonTargetNotFound, fmt.Sprintf("target %s not found", key.String()))
}

// setConfigsNotReady sets Ready=False with a target stage reason on the
// Configs of the target. The message is bounded by the condition constructor.
// A failed status update is returned, so the request is retried with backoff.
func (r *reconciler) setConfigsNotReady(ctx context.Context, target *configv1alpha1.Target, reason condv1alpha1.ConditionReason, msg string) error {
	log.FromContext(ctx).Debug("setConfigsNotReady", "reason", reason, "msg", msg)
	return errors.Wrap(r.transactor.SetConfigsNotReady(ctx, target,
		configv1alpha1.ConfigNotReady(reason, msg)), errUpdateStatus)
}
