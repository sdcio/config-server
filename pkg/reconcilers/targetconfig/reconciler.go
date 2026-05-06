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
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/pkg/errors"
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	configv1alpha1apply "github.com/sdcio/config-server/pkg/generated/applyconfiguration/config/v1alpha1"
	"github.com/sdcio/config-server/pkg/keyring"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	targetmanager "github.com/sdcio/config-server/pkg/sdc/target/manager"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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
)

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
	if cfg.KeyRing == nil {
		return nil, fmt.Errorf("KeyRing is nil: required for SensitiveConfig decryption")
	}

	r.client = mgr.GetClient()
	r.keyring = cfg.KeyRing
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
	r.transactor = targetmanager.NewTransactor()
	// we need to use a common fieldmanager for config and config recovery due to SSA
	r.cfgMgr = targetmanager.NewConfigManager(mgr.GetClient(), "targetConfigManager")

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(reconcilerName).
		For(&configv1alpha1.Target{}).
		// SensitiveConfig change is the primary trigger — resolver has already
		// fetched secrets and detected changes before we get here.
		Watches(&configv1alpha1.SensitiveConfig{},
			handler.EnqueueRequestsFromMapFunc(r.mapSensitiveConfigToTarget),
		).
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

	// ── Deletion ──────────────────────────────────────────────────────────────
	if !targetOrig.GetDeletionTimestamp().IsZero() {
		if err := r.cfgMgr.SetConfigsTargetConditionForTarget(
			ctx, targetOrig,
			configv1alpha1.TargetForConfigFailed("target not available"),
		); err != nil {
			return ctrl.Result{Requeue: true},
				errors.Wrap(r.handleError(ctx, targetOrig, "cannot update config status", err), errUpdateStatus)
		}
		if err := r.finalizer.RemoveFinalizer(ctx, targetOrig); err != nil {
			return ctrl.Result{Requeue: true},
				errors.Wrap(r.handleError(ctx, targetOrig, "cannot delete finalizer", err), errUpdateStatus)
		}
		log.Debug("successfully deleted resource")
		return ctrl.Result{}, nil
	}

	if err := r.finalizer.AddFinalizer(ctx, targetOrig); err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, targetOrig, "cannot add finalizer", err), errUpdateStatus)
	}

	// ── Target readiness ──────────────────────────────────────────────────────
	if !targetOrig.IsReady() {
		err := r.cfgMgr.SetConfigsTargetConditionForTarget(ctx, targetOrig,
			configv1alpha1.TargetForConfigFailed("target not ready"))
		return ctrl.Result{RequeueAfter: 5 * time.Second},
			errors.Wrap(r.handleError(ctx, targetOrig, "target not ready", err), errUpdateStatus)
	}

	dsctx, ok := r.targetMgr.GetDatastore(ctx, targetKey)
	if !ok || dsctx == nil {
		err := r.cfgMgr.SetConfigsTargetConditionForTarget(ctx, targetOrig,
			configv1alpha1.TargetForConfigFailed("target not ready (no dsctx yet)"))
		return ctrl.Result{RequeueAfter: 5 * time.Second},
			errors.Wrap(r.handleError(ctx, targetOrig, "target runtime not ready (no dsctx yet)", err), errUpdateStatus)
	}
	if dsctx.Client == nil {
		err := r.cfgMgr.SetConfigsTargetConditionForTarget(ctx, targetOrig,
			configv1alpha1.TargetForConfigFailed("target not ready (no dsctx client)"))
		return ctrl.Result{RequeueAfter: 5 * time.Second},
			errors.Wrap(r.handleError(ctx, targetOrig,
				fmt.Sprintf("target runtime not ready phase=%s dsReady=%t dsStoreReady=%t recovered=%t err=%v",
					dsctx.Status.Phase, dsctx.Status.DSReady, dsctx.Status.DSStoreReady,
					dsctx.Status.Recovered, dsctx.Status.LastError),
				err), errUpdateStatus)
	}
	if !dsctx.Status.Recovered {
		err := r.cfgMgr.SetConfigsTargetConditionForTarget(ctx, targetOrig,
			configv1alpha1.TargetForConfigFailed("target not recovered"))
		log.Info("config transaction -> target not recovered yet")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, err
	}

	// ── Load data sources ─────────────────────────────────────────────────────

	// SensitiveConfigs carry resolved (encrypted) blobs — primary data source.
	scList, err := r.listSensitiveConfigsPerTarget(ctx, targetOrig)
	if err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, targetOrig, "cannot list SensitiveConfigs", err), errUpdateStatus)
	}

	// Configs still needed for: status writes, recoverability, Lifecycle, no-secret blobs.
	cfgList, err := r.cfgMgr.ListConfigsPerTarget(ctx, targetOrig)
	if err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, targetOrig, "cannot list Configs", err), errUpdateStatus)
	}

	// TargetSnapshot holds the last successfully applied resolved state.
	snapshot, err := r.loadSnapshot(ctx, targetOrig)
	if err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, targetOrig, "cannot load snapshot", err), errUpdateStatus)
	}

	// ── Change detection + intent building ────────────────────────────────────

	toUpdate, toDelete, hasChange, err := r.buildIntentInputs(ctx, scList, cfgList, snapshot)
	if err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, targetOrig, "cannot build intent inputs", err), errUpdateStatus)
	}

	if !hasChange {
		log.Info("Transact skip, nothing to update")
		return ctrl.Result{}, nil
	}

	// Reapply deviations before transacting.
	/*
	for _, cfg := range cfgList.Items {
		if _, err := r.cfgMgr.ApplyDeviation(ctx, &cfg); err != nil {
			return ctrl.Result{Requeue: true},
				errors.Wrap(r.handleError(ctx, targetOrig, "cannot apply deviation", err), errUpdateStatus)
		}
	}
	*/

	targetCond := configv1alpha1.TargetForConfigReady("target ready")

	// ── Build gRPC intents (pure transformation) ───────────────────────────────
	intents, err := targetmanager.BuildGRPCIntents(toUpdate, toDelete)
	if err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, targetOrig, "cannot build gRPC intents", err), errUpdateStatus)
	}

	// ── Execute ────────────────────────────────────────────────────────────────
	txID := uuid.New().String()
	rsp, txErr := r.transactor.Execute(ctx, dsctx, txID, intents, false)
	result := targetmanager.AnalyzeIntentResponse(txErr, rsp)

	for _, w := range result.GlobalWarnings {
		log.Warn("transaction warning", "warning", w)
	}

	if result.HasErrors() {
		log.Warn("transaction failed",
			"recoverable", result.Recoverable,
			"globalError", result.GlobalError,
			"intentErrors", result.IntentErrors,
		)
		retry, err := r.cfgMgr.ProcessErrors(ctx, rsp, toUpdate, toDelete, result.GlobalError, result.Recoverable)
		if retry {
			if condErr := r.cfgMgr.SetConfigsTargetConditionForTarget(ctx, targetOrig, targetCond); condErr != nil {
				return ctrl.Result{}, errors.Wrap(r.handleError(ctx, targetOrig, "", condErr), errUpdateStatus)
			}
			return ctrl.Result{RequeueAfter: 500 * time.Millisecond, Requeue: true},
				errors.Wrap(r.handleError(ctx, targetOrig, "", err), errUpdateStatus)
		}
		return ctrl.Result{}, errors.Wrap(r.handleError(ctx, targetOrig, "", err), errUpdateStatus)
	}

	// ── Confirm (gRPC only) ────────────────────────────────────────────────────
	if err := r.transactor.Confirm(ctx, dsctx, txID); err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, targetOrig, "cannot confirm transaction", err), errUpdateStatus)
	}

	log.Info("config transaction confirmed")

	// ── Update Config statuses (K8s only) ─────────────────────────────────────
	if err := r.cfgMgr.ProcessSuccess(ctx, toUpdate, toDelete, targetCond); err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, targetOrig, "cannot update config status after success", err), errUpdateStatus)
	}

	// ── Save snapshot ──────────────────────────────────────────────────────────
	// Not fatal if it fails — next reconcile will detect change and re-transact.
	if err := r.saveSnapshot(ctx, targetOrig, scList, toUpdate, toDelete); err != nil {
		log.Warn("cannot save snapshot after transaction", "err", err)
	}

	if err := r.cfgMgr.SetConfigsTargetConditionForTarget(ctx, targetOrig, targetCond); err != nil {
		return ctrl.Result{}, errors.Wrap(r.handleError(ctx, targetOrig, "", err), errUpdateStatus)
	}

	return ctrl.Result{}, errors.Wrap(r.handleSuccess(ctx, targetOrig), errUpdateStatus)
}

// ── Intent inputs ──────────────────────────────────────────────────────────────

// buildIntentInputs compares SensitiveConfigs against the TargetSnapshot to
// determine what needs to be sent to the datastore. Returns classified inputs
// and whether any change was detected.
func (r *reconciler) buildIntentInputs(
	ctx context.Context,
	scList *configv1alpha1.SensitiveConfigList,
	cfgList *config.ConfigList,
	snapshot *configv1alpha1.TargetSnapshot,
) (toUpdate []targetmanager.IntentInput, toDelete []targetmanager.IntentInput, hasChange bool, err error) {

	// Build lookup maps for fast access.
	scByName := make(map[string]*configv1alpha1.SensitiveConfig, len(scList.Items))
	for i := range scList.Items {
		scByName[scList.Items[i].Name] = &scList.Items[i]
	}

	var nonRecoverable []targetmanager.IntentInput

	for i := range cfgList.Items {
		cfg := &cfgList.Items[i]
		key := config.GetGVKNSN(cfg)

		// Deletion: deletionTimestamp set → always include as delete.
		if cfg.GetDeletionTimestamp() != nil {
			toDelete = append(toDelete, targetmanager.IntentInput{
				Config: cfg,
				Delete: true,
			})
			hasChange = true
			continue
		}

		sc, hasSC := scByName[cfg.Name]
		if !hasSC {
			// Resolver hasn't produced a SensitiveConfig yet — skip for now.
			// Resolver condition on Config will show the user why.
			log.FromContext(ctx).Info("no SensitiveConfig for config, skipping", "config", key)
			continue
		}

		// Get resolved blobs for this config (decrypt or use original).
		blobs, decErr := r.getResolvedBlobs(ctx, sc)
		if decErr != nil {
			log.FromContext(ctx).Warn("cannot get resolved blobs, skipping config", "config", key, "err", decErr)
			continue
		}

		input := targetmanager.IntentInput{
			Config:        cfg,
			ResolvedBlobs: blobs,
			Priority:      int32(sc.Spec.Priority),
			NonRevertive:  sc.Spec.Revertive != nil && !*sc.Spec.Revertive,
		}

		// Unrecoverable from past transaction: include only if other changes exist.
		if !cfg.IsRecoverable(ctx) {
			nonRecoverable = append(nonRecoverable, input)
			continue
		}

		// Detect change against snapshot.
		snapEntry, inSnap := snapshot.Spec.Configs[cfg.Name]
		changed := !inSnap ||
			sc.Spec.Payload.PlainHash != snapEntry.Payload.PlainHash ||
			sc.Spec.Priority != snapEntry.Priority ||
			!reflect.DeepEqual(sc.Spec.Revertive, snapEntry.Revertive) ||
			!reflect.DeepEqual(sc.Spec.Lifecycle, snapEntry.Lifecycle) ||
			r.keyring.NeedsReencryption(snapEntry.Payload)

		// Also include if config condition is not ready (retry failed transaction).
		if changed || !cfg.IsConfigConditionReady() {
			hasChange = true
			toUpdate = append(toUpdate, input)
		}
	}

	// Check for snapshot entries with no current SensitiveConfig → deleted configs.
	for name := range snapshot.Spec.Configs {
		if _, hasSC := scByName[name]; !hasSC {
			// SC was deleted (Config deleted → ownerRef GC removed SC).
			// Find the Config to mark as delete intent if still exists.
			hasChange = true
			// The Config may already be in toDelete above if deletionTimestamp is set.
			// If the SC is gone but Config is not, treat as no-op (GC handles cleanup).
		}
	}

	// Include unrecoverable configs when other changes were found.
	// This gives them another chance — content may now be valid.
	if hasChange {
		toUpdate = append(toUpdate, nonRecoverable...)
	}

	return toUpdate, toDelete, hasChange, nil
}

// getResolvedBlobs returns the resolved ConfigBlobs as the internal type
// so they can be passed directly to config.GetIntentUpdateFromBlobs.
func (r *reconciler) getResolvedBlobs(ctx context.Context, sc *configv1alpha1.SensitiveConfig) ([]config.ConfigBlob, error) {
	if sc.Spec.Payload.KeyID == "" || len(sc.Spec.Payload.Data) == 0 {
		// No secrets — fetch original blobs from Config and convert to internal type.
		v1cfg := &configv1alpha1.Config{}
		if err := r.client.Get(ctx,
			types.NamespacedName{Name: sc.Name, Namespace: sc.Namespace},
			v1cfg,
		); err != nil {
			return nil, fmt.Errorf("get config for no-secret SC %s: %w", sc.Name, err)
		}
		// Convert []configv1alpha1.ConfigBlob → []config.ConfigBlob
		// Both types have identical JSON structure (Path + Value).
		blobs := make([]config.ConfigBlob, len(v1cfg.Spec.Config))
		for i, b := range v1cfg.Spec.Config {
			blobs[i] = config.ConfigBlob{Path: b.Path, Value: b.Value}
		}
		return blobs, nil
	}

	plain, err := r.keyring.Decrypt(sc.Spec.Payload)
	if err != nil {
		return nil, fmt.Errorf("decrypt SC %s: %w", sc.Name, err)
	}
	// Unmarshal directly as internal type — JSON structure is identical.
	var blobs []config.ConfigBlob
	if err := json.Unmarshal(plain, &blobs); err != nil {
		return nil, fmt.Errorf("unmarshal resolved blobs for SC %s: %w", sc.Name, err)
	}
	return blobs, nil
}

// ── Snapshot management ────────────────────────────────────────────────────────

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
		// Not found — return empty snapshot (first time).
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

// saveSnapshot persists the TargetSnapshot after a successful transaction.
// It updates only the entries that were part of this transaction, and removes
// entries for deleted configs.
func (r *reconciler) saveSnapshot(
	ctx context.Context,
	target *configv1alpha1.Target,
	scList *configv1alpha1.SensitiveConfigList,
	toUpdate []targetmanager.IntentInput,
	toDelete []targetmanager.IntentInput,
) error {
	// Build a lookup of what was updated.
	updatedNames := make(map[string]struct{}, len(toUpdate))
	for _, u := range toUpdate {
		updatedNames[u.Config.Name] = struct{}{}
	}
	deletedNames := make(map[string]struct{}, len(toDelete))
	for _, d := range toDelete {
		deletedNames[d.Config.Name] = struct{}{}
	}

	// Build the complete new snapshot from current SensitiveConfigs.
	// SC spec already contains the correctly encrypted payload with current key.
	scByName := make(map[string]configv1alpha1.SensitiveConfigSpec, len(scList.Items))
	for _, sc := range scList.Items {
		if _, wasDeleted := deletedNames[sc.Name]; !wasDeleted {
			scByName[sc.Name] = sc.Spec
		}
	}

	desired := &configv1alpha1.TargetSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      target.Name,
			Namespace: target.Namespace,
		},
		Spec: configv1alpha1.TargetSnapshotSpec{
			Configs: scByName,
		},
	}

	existing := &configv1alpha1.TargetSnapshot{}
	err := r.client.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return err
		}
		return r.client.Create(ctx, desired)
	}
	existing.Spec = desired.Spec
	return r.client.Update(ctx, existing)
}

// ── List helpers ───────────────────────────────────────────────────────────────

func (r *reconciler) listSensitiveConfigsPerTarget(ctx context.Context, target *configv1alpha1.Target) (*configv1alpha1.SensitiveConfigList, error) {
	scList := &configv1alpha1.SensitiveConfigList{}
	if err := r.client.List(ctx, scList,
		client.InNamespace(target.GetNamespace()),
		client.MatchingLabels{
			config.TargetNamespaceKey: target.GetNamespace(),
			config.TargetNameKey:      target.GetName(),
		},
	); err != nil {
		return nil, err
	}
	return scList, nil
}

// mapSensitiveConfigToTarget maps a SensitiveConfig event to its Target.
func (r *reconciler) mapSensitiveConfigToTarget(_ context.Context, obj client.Object) []reconcile.Request {
	labels := obj.GetLabels()
	targetNS, ok1 := labels[config.TargetNamespaceKey]
	targetName, ok2 := labels[config.TargetNameKey]
	if !ok1 || !ok2 {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Name:      targetName,
			Namespace: targetNS,
		},
	}}
}

// ── Handlers ───────────────────────────────────────────────────────────────────

func (r *reconciler) handleSuccess(ctx context.Context, target *configv1alpha1.Target) error {
	log.FromContext(ctx).Debug("handleSuccess", "key", target.GetNamespacedName())
	return nil
}

func (r *reconciler) handleError(ctx context.Context, target *configv1alpha1.Target, msg string, err error) error {
	log := log.FromContext(ctx)
	if err != nil {
		msg = fmt.Sprintf("%q err %q", msg, err.Error())
	}
	if len(msg) > 128 {
		msg = msg[:128]
	}
	log.Warn("config transaction failed", "msg", msg, "err", err)
	return nil
}
