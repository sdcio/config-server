/*
Copyright 2024 Nokia.
...
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
	"github.com/sdcio/config-server/pkg/keyring"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	targetmanager "github.com/sdcio/config-server/pkg/sdc/target/manager"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
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
	fieldmanagerfinalizer = "TargetRecoveryConfigController-finalizer"
	reconcilerName        = "TargetRecoveryConfigController"
	finalizer             = "targetrecoveryserver.inv.sdcio.dev/finalizer"
	errGetCr              = "cannot get cr"
	errUpdateStatus       = "cannot update status"
)

func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {
	cfg, ok := c.(*ctrlconfig.ControllerConfig)
	if !ok {
		return nil, fmt.Errorf("cannot initialize, expecting controllerConfig, got: %s", reflect.TypeOf(c).Name())
	}
	if cfg.TargetManager == nil {
		return nil, fmt.Errorf("TargetManager is nil: set LOCAL_DATASERVER=true or disable TargetRecoveryServerController")
	}
	if cfg.KeyRing == nil {
		return nil, fmt.Errorf("KeyRing is nil: required for snapshot decryption during recovery")
	}

	var err error
	r.discoveryClient, err = ctrlconfig.GetDiscoveryClient(mgr)
	if err != nil {
		return nil, fmt.Errorf("cannot get discoveryClient from manager")
	}

	r.client    = mgr.GetClient()
	r.keyring   = cfg.KeyRing
	r.targetMgr = cfg.TargetManager
	r.recorder  = mgr.GetEventRecorder(reconcilerName)
	r.transactor = targetmanager.NewTransactor()
	// we need to use a common fieldmanager for config and config recovery due to SSA
	r.cfgMgr     = targetmanager.NewConfigManager(mgr.GetClient(), "targetConfigManager")
	r.finalizer  = resource.NewAPIFinalizer(
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
	cfgMgr          *targetmanager.ConfigManager
	keyring         *keyring.KeyRing
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
		if resource.IgnoreNotFound(err) != nil {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}
	targetOrig := target.DeepCopy()

	if !targetOrig.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, nil
	}

	dsctx, ok := r.targetMgr.GetDatastore(ctx, targetKey)
	if !ok || dsctx == nil {
		return ctrl.Result{RequeueAfter: 5 * time.Second},
			errors.Wrap(r.handleError(ctx, targetOrig, "target runtime not ready (no dsctx yet)", nil), errUpdateStatus)
	}
	if dsctx.Client == nil {
		return ctrl.Result{RequeueAfter: 5 * time.Second},
			errors.Wrap(r.handleError(ctx, targetOrig,
				fmt.Sprintf("target runtime not ready phase=%s dsReady=%t dsStoreReady=%t recovered=%t err=%v",
					dsctx.Status.Phase, dsctx.Status.DSReady, dsctx.Status.DSStoreReady,
					dsctx.Status.Recovered, dsctx.Status.LastError),
				nil), errUpdateStatus)
	}

	if dsctx.Status.Recovered {
		log.Info("config recovery → already recovered")
		return ctrl.Result{}, errors.Wrap(r.handleSuccess(ctx, targetOrig, nil), errUpdateStatus)
	}

	// ── Load TargetSnapshot — source of truth for recovery blobs ──────────────
	snapshot, err := r.loadSnapshot(ctx, targetOrig)
	if err != nil {
		return ctrl.Result{RequeueAfter: 5 * time.Second},
			errors.Wrap(r.handleError(ctx, targetOrig, "cannot load snapshot", err), errUpdateStatus)
	}

	// ── Recover using snapshot blobs ───────────────────────────────────────────
	msg, err := targetmanager.RecoverConfigs(ctx, targetOrig, dsctx, r.transactor, r.cfgMgr, r.keyring, snapshot)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(r.handleError(ctx, targetOrig, "recovery failed", err), errUpdateStatus)
	}

	return ctrl.Result{}, errors.Wrap(r.handleSuccess(ctx, targetOrig, msg), errUpdateStatus)
}

// ── Snapshot loader ────────────────────────────────────────────────────────────

func (r *reconciler) loadSnapshot(ctx context.Context, target *configv1alpha1.Target) (*configv1alpha1.TargetSnapshot, error) {
	snapshot := &configv1alpha1.TargetSnapshot{}
	err := r.client.Get(ctx,
		types.NamespacedName{Name: target.Name, Namespace: target.Namespace},
		snapshot,
	)
	if err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return nil, err
		}
		// No snapshot yet — nothing was ever successfully applied.
		return &configv1alpha1.TargetSnapshot{
			Spec: configv1alpha1.TargetSnapshotSpec{
				Configs: map[string]configv1alpha1.SensitiveConfigSpec{},
			},
		}, nil
	}
	if snapshot.Spec.Configs == nil {
		snapshot.Spec.Configs = map[string]configv1alpha1.SensitiveConfigSpec{}
	}
	return snapshot, nil
}

// ── Handlers ───────────────────────────────────────────────────────────────────

func (r *reconciler) handleSuccess(ctx context.Context, target *configv1alpha1.Target, msg *string) error {
	log := log.FromContext(ctx)
	newMsg := ""
	if msg != nil {
		newMsg = *msg
	}
	newCond := configv1alpha1.TargetConfigRecoveryReady(newMsg)
	oldCond := target.GetCondition(configv1alpha1.ConditionTypeTargetConfigRecoveryReady)
	if newCond.Equal(oldCond) {
		log.Info("handleSuccess → no change")
		return nil
	}
	log.Info("handleSuccess → change")
	r.recorder.Eventf(target, nil, corev1.EventTypeNormal, configv1alpha1.TargetKind, "config recovery ready", "")
	applyConfig := configv1alpha1apply.Target(target.Name, target.Namespace).
		WithStatus(configv1alpha1apply.TargetStatus().WithConditions(newCond))
	return r.client.Status().Apply(ctx, applyConfig, &client.SubResourceApplyOptions{
		ApplyOptions: client.ApplyOptions{FieldManager: reconcilerName, Force: ptr.To(true)},
	})
}

func (r *reconciler) handleError(ctx context.Context, target *configv1alpha1.Target, msg string, err error) error {
	log := log.FromContext(ctx)
	if err != nil {
		msg = fmt.Sprintf("%q err %q", msg, err.Error())
	}
	if len(msg) > 128 {
		msg = msg[:128]
	}
	newCond := configv1alpha1.TargetConfigRecoveryFailed(msg)
	oldCond := target.GetCondition(configv1alpha1.ConditionTypeTargetConfigRecoveryReady)
	if newCond.Equal(oldCond) {
		log.Info("handleError → no change")
		return nil
	}
	log.Warn(msg, "error", err)
	r.recorder.Eventf(target, nil, corev1.EventTypeWarning, configv1alpha1.TargetKind, msg, "")
	applyConfig := configv1alpha1apply.Target(target.Name, target.Namespace).
		WithStatus(configv1alpha1apply.TargetStatus().WithConditions(newCond))
	return r.client.Status().Apply(ctx, applyConfig, &client.SubResourceApplyOptions{
		ApplyOptions: client.ApplyOptions{FieldManager: reconcilerName, Force: ptr.To(true)},
	})
}