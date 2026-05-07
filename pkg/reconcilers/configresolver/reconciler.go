/*
Copyright 2026 Nokia.

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

package configresolver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/henderiw/logger/log"
	"github.com/pkg/errors"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	configv1alpha1apply "github.com/sdcio/config-server/pkg/generated/applyconfiguration/config/v1alpha1"
	"github.com/sdcio/config-server/pkg/keyring"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func init() {
	reconcilers.Register(crName, &reconciler{})
}

const (
	crName                = "resolver"
	fieldmanagerfinalizer = "SensitiveResolverController-finalizer"
	reconcilerName        = "SensitiveResolverController"
	finalizer             = "sensitiveresolver.config.sdcio.dev/finalizer"
	refLabelPrefix        = "config.sdcio.dev/ref."
	// requeue delay when secrets are unavailable — Secret watch won't help
	// if SensitiveConfig doesn't exist yet (no ref labels set)
	requeueOnResolutionFailure = 30 * time.Second
	// errors
	errGetCr        = "cannot get cr"
	errUpdateStatus = "cannot update status"
)

var secretRefPattern = regexp.MustCompile(`^secret::([^:]+)::(.+)$`)

// ── Change detection ───────────────────────────────────────────────────────────

// changeResult holds the outcome of detectChange.
// All three criteria are always evaluated — no early exit — so annotations
// accurately reflect every dimension of what changed simultaneously.
type changeResult struct {
	configChanged  bool
	secretChanged  bool
	keyringChanged bool
}

// needsResolution returns true when secret fetching and substitution is required.
func (cr changeResult) needsResolution() bool {
	return cr.configChanged || cr.secretChanged
}

// needsReencryptionOnly returns true when only the keyring changed.
// The existing payload can be re-encrypted without fetching any secrets.
func (cr changeResult) needsReencryptionOnly() bool {
	return cr.keyringChanged && !cr.needsResolution()
}

// noChange returns true when nothing requires action.
func (cr changeResult) noChange() bool {
	return !cr.configChanged && !cr.secretChanged && !cr.keyringChanged
}

// ── SetupWithManager ───────────────────────────────────────────────────────────

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

	if cfg.KeyRing == nil {
		return nil, fmt.Errorf("KeyRing is nil: set keyring secret or disable SensitiveResolverController")
	}

	r.client = mgr.GetClient()
	r.apiReader = mgr.GetAPIReader()
	r.keyring = cfg.KeyRing
	r.recorder = mgr.GetEventRecorder(reconcilerName)
	r.finalizer = resource.NewAPIFinalizer(
		mgr.GetClient(),
		finalizer,
		fieldmanagerfinalizer,
		func(name, namespace string, finalizers ...string) runtime.ApplyConfiguration {
			ac := configv1alpha1apply.Config(name, namespace)
			if len(finalizers) > 0 {
				ac.WithFinalizers(finalizers...)
			}
			return ac
		},
	)

	isKeyring := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		_, ok := obj.GetLabels()[config.LabelKeyRingKey]
		return ok
	})
	notKeyring := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		_, ok := obj.GetLabels()[config.LabelKeyRingKey]
		return !ok
	})

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(reconcilerName).
		For(&configv1alpha1.Config{}).
		Owns(&configv1alpha1.SensitiveConfig{}).
		WatchesMetadata(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.mapSecretToConfigs),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}, notKeyring),
		).
		WatchesMetadata(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.mapKeyRingToAllConfigs),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}, isKeyring),
		).
		Complete(r)
}

type reconciler struct {
	client          client.Client
	apiReader       client.Reader
	discoveryClient *discovery.DiscoveryClient
	finalizer       *resource.APIFinalizer
	keyring         *keyring.KeyRing
	recorder        events.EventRecorder
}

// ── Reconcile ──────────────────────────────────────────────────────────────────

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, reconcilerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	if _, err := r.discoveryClient.ServerResourcesForGroupVersion(configv1alpha1.SchemeGroupVersion.String()); err != nil {
		log.Info("API group not available, retrying...", "groupversion", configv1alpha1.SchemeGroupVersion.String())
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	cfg := &configv1alpha1.Config{}
	if err := r.client.Get(ctx, req.NamespacedName, cfg); err != nil {
		if resource.IgnoreNotFound(err) != nil {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}
	cfgOrig := cfg.DeepCopy()

	// ── Deletion ──────────────────────────────────────────────────────────────
	if !cfgOrig.GetDeletionTimestamp().IsZero() {
		// Do NOT delete the SC here. It must remain alive until the targetconfig
		// controller confirms the datastore deletion. It serves as:
		//   - The record that this config was applied and must be removed from device
		//   - The surface for failure conditions if the delete fails
		// The targetconfig controller deletes it explicitly after TransactionConfirm.
		if err := r.finalizer.RemoveFinalizer(ctx, cfgOrig); err != nil {
			return ctrl.Result{Requeue: true},
		errors.Wrap(r.handleError(ctx, cfgOrig, "cannot remove finalizer", err), errUpdateStatus)
    }
    log.Debug("removed resolver finalizer, SC retained until datastore deletion confirmed")
    return ctrl.Result{}, nil
	}

	// ── Finalizer ─────────────────────────────────────────────────────────────
	if err := r.finalizer.AddFinalizer(ctx, cfgOrig); err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, cfgOrig, "cannot add finalizer", err), errUpdateStatus)
	}

	// ── Load existing SensitiveConfig ─────────────────────────────────────────
	existingSC := &configv1alpha1.SensitiveConfig{}
	err := r.client.Get(ctx, req.NamespacedName, existingSC)
	if err != nil && resource.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, errors.Wrap(r.handleError(ctx, cfgOrig, "cannot get SensitiveConfig", err), errGetCr)
	}
	hasExistingSC := err == nil

	// ── Detect change: walks ALL criteria without early exit ──────────────────
	change, fetched, err := r.detectChange(ctx, cfgOrig, existingSC, hasExistingSC)
	if err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, cfgOrig, "cannot detect change", err), errUpdateStatus)
	}
	log.Info("change detection result",
		"configChanged", change.configChanged,
		"secretChanged", change.secretChanged,
		"keyringChanged", change.keyringChanged,
	)

	// ── No change ─────────────────────────────────────────────────────────────
	if change.noChange() {
		log.Debug("no change detected, skipping")
		return ctrl.Result{}, nil
	}

	// ── Keyring rotation only: re-encrypt, no secret fetch needed ─────────────
	if change.needsReencryptionOnly() {
		if err := r.reencrypt(ctx, existingSC); err != nil {
			return ctrl.Result{Requeue: true},
				errors.Wrap(r.handleError(ctx, cfgOrig, "cannot re-encrypt SensitiveConfig", err), errUpdateStatus)
		}
		return ctrl.Result{}, errors.Wrap(r.handleSuccess(ctx, cfgOrig), errUpdateStatus)
	}

	// ── Resolution needed ─────────────────────────────────────────────────────
	// Covers: configChanged, secretChanged, or both (+ possible concurrent keyringChanged).
	// r.keyring.Encrypt always uses the primary key, so a concurrent keyring
	// rotation is handled automatically during resolution.
	refs := parseSecretRefs(cfgOrig.Spec.Config)

	// GetHash hashes only the ConfigSpec — defined in config_helpers.go
	configHash, err := cfgOrig.Spec.GetHash()
	if err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, cfgOrig, "cannot hash config blobs", err), errUpdateStatus)
	}

	// Pre-fetched secrets from detectChange are reused here to avoid double-fetching.
	payload, secretKeyHashes, err := r.buildPayload(ctx, cfgOrig, refs, fetched)
	if err != nil {
		// Resolution failed: secret missing, key missing, or encryption error.
		// Set Resolver condition so the user sees the problem immediately.
		// Do NOT update SensitiveConfig — preserve the last known good resolved state
		// so the remote controller continues operating on valid data.
		// RequeueAfter ensures retry even when no Secret event fires
		// (e.g. the secret still needs to be created from scratch).
		_ = r.handleError(ctx, cfgOrig, "cannot resolve secrets", err)
		return ctrl.Result{RequeueAfter: requeueOnResolutionFailure}, nil
	}

	if err := r.save(ctx, cfgOrig, payload, configHash, secretKeyHashes, refs, change); err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, cfgOrig, "cannot save SensitiveConfig", err), errUpdateStatus)
	}

	return ctrl.Result{}, errors.Wrap(r.handleSuccess(ctx, cfgOrig), errUpdateStatus)
}

// ── detectChange ──────────────────────────────────────────────────────────────

// detectChange evaluates all three criteria without early exit.
// Returns the full picture for accurate per-dimension annotations.
// Also returns pre-fetched secrets to avoid double-fetching in buildPayload.
func (r *reconciler) detectChange(
	ctx context.Context,
	cfg *configv1alpha1.Config,
	existing *configv1alpha1.SensitiveConfig,
	hasExisting bool,
) (changeResult, map[string]*corev1.Secret, error) {
	if !hasExisting {
		// First time or externally deleted — full resolution required.
		return changeResult{configChanged: true}, nil, nil
	}

	result := changeResult{}
	fetched := make(map[string]*corev1.Secret)

	// 1. Keyring: always check — no API call needed.
	result.keyringChanged = r.keyring.NeedsReencryption(existing.Spec.Payload)

	// 2. Config blobs hash: detects any structural change — new/removed refs,
	//    changed paths, changed non-secret values.
	currentConfigHash, err := cfg.Spec.GetHash()
	if err != nil {
		return result, nil, fmt.Errorf("hash config blobs: %w", err)
	}
	result.configChanged = currentConfigHash != existing.Spec.ConfigHash

	// 3. Secret key hashes: only meaningful when config structure is unchanged.
	//    If config changed, stored hashes reference potentially stale refs —
	//    full resolution will be done regardless, so skip secret detection.
	//    When config is stable, check the hash of each specific secret key value.
	if !result.configChanged && len(existing.Spec.SecretKeyHashes) > 0 {
		for refKey, storedHash := range existing.Spec.SecretKeyHashes {
			secretName, keyName, err := splitRefKey(refKey)
			if err != nil {
				continue
			}
			if _, ok := fetched[secretName]; !ok {
				secret := &corev1.Secret{}
				if err := r.apiReader.Get(ctx,
					types.NamespacedName{Name: secretName, Namespace: cfg.Namespace},
					secret,
				); err != nil {
					// Secret deleted or unavailable → treat as changed.
					result.secretChanged = true
					continue // keep checking others for accurate annotations
				}
				fetched[secretName] = secret
			}
			if sha256hex(fetched[secretName].Data[keyName]) != storedHash {
				result.secretChanged = true
				// No break — walk ALL keys for accurate annotations.
			}
		}
	}

	return result, fetched, nil
}

// ── reencrypt ─────────────────────────────────────────────────────────────────

func (r *reconciler) reencrypt(ctx context.Context, sc *configv1alpha1.SensitiveConfig) error {
	if len(sc.Spec.Payload.Data) == 0 {
		// Nothing to re-encrypt (no-secret-refs entry).
		return nil
	}
	plain, err := r.keyring.Decrypt(sc.Spec.Payload)
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}
	newPayload, err := r.keyring.Encrypt(plain)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}
	// PlainHash, ConfigHash, SecretKeyHashes are all unchanged — only the
	// wrapping key changed, not the content.
	newPayload.PlainHash = sc.Spec.Payload.PlainHash
	sc.Spec.Payload = newPayload

	metav1.SetMetaDataAnnotation(&sc.ObjectMeta, config.AnnotationConfigChanged, "false")
	metav1.SetMetaDataAnnotation(&sc.ObjectMeta, config.AnnotationSecretChanged, "false")
	metav1.SetMetaDataAnnotation(&sc.ObjectMeta, config.AnnotationKeyringChanged, "true")
	return r.client.Update(ctx, sc)
}

// ── buildPayload ──────────────────────────────────────────────────────────────

// buildPayload resolves secrets and encrypts the result.
// Accepts pre-fetched secrets from detectChange to avoid double-fetching.
// Returns the encrypted payload and per-key hashes for future change detection.
func (r *reconciler) buildPayload(
	ctx context.Context,
	cfg *configv1alpha1.Config,
	refs []SecretRef,
	fetched map[string]*corev1.Secret,
) (configv1alpha1.EncryptedPayload, map[string]string, error) {
	if len(refs) == 0 {
		// No secret refs, but we still encrypt so the snapshot is self-contained.
		// Without this, recovery would fall back to cfg.Spec.Config which may
		// have changed since the snapshot was taken (issue: stale recovery state).
		raw, err := json.Marshal(cfg.Spec.Config)
		if err != nil {
			return configv1alpha1.EncryptedPayload{}, nil, err
		}
		plainHash := sha256hex(raw)
		payload, err := r.keyring.Encrypt(raw)
		if err != nil {
			return configv1alpha1.EncryptedPayload{}, nil, err
		}
		payload.PlainHash = plainHash
		return payload, nil, nil
	}

	resolvedData, plainHash, secretKeyHashes, err := r.resolve(ctx, cfg, refs, fetched)
	if err != nil {
		return configv1alpha1.EncryptedPayload{}, nil, err
	}

	payload, err := r.keyring.Encrypt(resolvedData)
	if err != nil {
		return configv1alpha1.EncryptedPayload{}, nil, err
	}
	payload.PlainHash = plainHash
	return payload, secretKeyHashes, nil
}

// ── resolve ───────────────────────────────────────────────────────────────────

// resolve fetches any secrets not already in fetched, substitutes all refs,
// and returns the marshaled resolved blobs, their hash, and per-key hashes.
func (r *reconciler) resolve(
	ctx context.Context,
	cfg *configv1alpha1.Config,
	refs []SecretRef,
	fetched map[string]*corev1.Secret,
) (data []byte, plainHash string, secretKeyHashes map[string]string, err error) {
	secretValues := make(map[string]string)
	secretKeyHashes = make(map[string]string)

	for _, ref := range refs {
		if _, ok := fetched[ref.SecretName]; !ok {
			secret := &corev1.Secret{}
			if err = r.apiReader.Get(ctx,
				types.NamespacedName{Name: ref.SecretName, Namespace: cfg.Namespace},
				secret,
			); err != nil {
				if resource.IgnoreNotFound(err) == nil {
					// Clear, actionable message for the user.
					return nil, "", nil, fmt.Errorf(
						"secret %q not found in namespace %q — create the secret to resolve this config",
						ref.SecretName, cfg.Namespace,
					)
				}
				return nil, "", nil, fmt.Errorf("fetch secret %q: %w", ref.SecretName, err)
			}
			fetched[ref.SecretName] = secret
		}

		val, ok := fetched[ref.SecretName].Data[ref.SecretKey]
		if !ok {
			// Secret exists but the referenced key is absent.
			return nil, "", nil, fmt.Errorf(
				"secret %q exists but has no key %q — add the missing key to resolve this config",
				ref.SecretName, ref.SecretKey,
			)
		}

		refKey := ref.SecretName + "/" + ref.SecretKey
		secretValues[refKey] = string(val)
		secretKeyHashes[refKey] = sha256hex(val) // hash of the specific key value only
	}

	resolved, err := substituteRefs(cfg.Spec.Config, secretValues)
	if err != nil {
		return nil, "", nil, err
	}
	data, err = json.Marshal(resolved)
	if err != nil {
		return nil, "", nil, err
	}
	return data, sha256hex(data), secretKeyHashes, nil
}

// ── save ──────────────────────────────────────────────────────────────────────

func (r *reconciler) save(
	ctx context.Context,
	cfg *configv1alpha1.Config,
	payload configv1alpha1.EncryptedPayload,
	configHash string,
	secretKeyHashes map[string]string,
	refs []SecretRef,
	change changeResult,
) error {
	labels := map[string]string{
		config.TargetNamespaceKey: cfg.Labels[config.TargetNamespaceKey],
		config.TargetNameKey:      cfg.Labels[config.TargetNameKey],
	}
	for _, ref := range refs {
		labels[refLabelPrefix+ref.SecretName] = "true"
	}

	desired := &configv1alpha1.SensitiveConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfg.Name,
			Namespace: cfg.Namespace,
			Labels:    labels,
			Annotations: map[string]string{
				config.AnnotationConfigChanged:  strconv.FormatBool(change.configChanged),
				config.AnnotationSecretChanged:  strconv.FormatBool(change.secretChanged),
				config.AnnotationKeyringChanged: strconv.FormatBool(change.keyringChanged),
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(cfg,
					configv1alpha1.SchemeGroupVersion.WithKind(configv1alpha1.ConfigKind),
				),
			},
		},
		Spec: configv1alpha1.SensitiveConfigSpec{
			Generation:      cfg.Generation,
			Lifecycle:       cfg.Spec.Lifecycle,
			Priority:        int64(cfg.Spec.Priority),
			Revertive:       cfg.Spec.Revertive,
			ConfigHash:      configHash,
			SecretKeyHashes: secretKeyHashes,
			Payload:         payload,
		},
	}

	existing := &configv1alpha1.SensitiveConfig{}
	err := r.client.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return err
		}
		return r.client.Create(ctx, desired)
	}
	existing.Labels = desired.Labels
	existing.Annotations = desired.Annotations
	existing.Spec = desired.Spec
	return r.client.Update(ctx, existing)
}

// ── Condition management ───────────────────────────────────────────────────────

// setResolverCondition applies only the Resolver condition via SSA.
// The overall Ready condition is owned by a different controller and is
// not touched here.
func (r *reconciler) setResolverCondition(ctx context.Context, cfg *configv1alpha1.Config, cond condv1alpha1.Condition) error {
	applyConfig := configv1alpha1apply.Config(cfg.Name, cfg.Namespace).
		WithStatus(configv1alpha1apply.ConfigStatus().
			WithConditions(cond),
		)
	return r.client.Status().Apply(ctx, applyConfig, &client.SubResourceApplyOptions{
		ApplyOptions: client.ApplyOptions{
			FieldManager: fieldmanagerfinalizer,
		},
	})
}

func (r *reconciler) handleSuccess(ctx context.Context, cfg *configv1alpha1.Config) error {
	log.FromContext(ctx).Debug("handleSuccess", "key", cfg.GetName())
	return r.setResolverCondition(ctx, cfg, configv1alpha1.ConfigResolverReady(""))
}

func (r *reconciler) handleError(ctx context.Context, cfg *configv1alpha1.Config, msg string, err error) error {
	log := log.FromContext(ctx)
	if err != nil {
		msg = fmt.Sprintf("%s: %s", msg, err.Error())
	}
	if len(msg) > 128 {
		msg = msg[:128]
	}
	log.Warn("sensitive resolver failed", "msg", msg, "err", err)
	return r.setResolverCondition(ctx, cfg, configv1alpha1.ConfigResolverFailed(msg))
}

// ── Event mappers ──────────────────────────────────────────────────────────────

// mapSecretToConfigs maps a Secret metadata event to Configs that still
// reference it. Uses ref labels on SensitiveConfig as a fast index, then
// verifies the ref is still live in the Config spec to guard against stale labels.
func (r *reconciler) mapSecretToConfigs(ctx context.Context, obj client.Object) []reconcile.Request {
	scList := &configv1alpha1.SensitiveConfigList{}
	if err := r.client.List(ctx, scList,
		client.InNamespace(obj.GetNamespace()),
		client.MatchingLabels{refLabelPrefix + obj.GetName(): "true"},
	); err != nil {
		return nil
	}

	var reqs []reconcile.Request
	for _, sc := range scList.Items {
		// Verify the secret is still referenced in the live Config spec.
		// Guards against stale labels between a Config update and next reconcile.
		cfg := &configv1alpha1.Config{}
		if err := r.client.Get(ctx, types.NamespacedName{
			Name:      sc.Name,
			Namespace: sc.Namespace,
		}, cfg); err != nil {
			continue
		}
		if configReferencesSecret(cfg.Spec.Config, obj.GetName()) {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      sc.Name,
					Namespace: sc.Namespace,
				},
			})
		}
	}
	return reqs
}

// mapKeyRingToAllConfigs reloads the KeyRing then enqueues every Config.
func (r *reconciler) mapKeyRingToAllConfigs(ctx context.Context, obj client.Object) []reconcile.Request {
	secret := &corev1.Secret{}
	if err := r.apiReader.Get(ctx, client.ObjectKeyFromObject(obj), secret); err != nil {
		return nil
	}
	if err := r.keyring.Reload(secret); err != nil {
		return nil
	}
	configList := &configv1alpha1.ConfigList{}
	if err := r.client.List(ctx, configList); err != nil {
		return nil
	}
	reqs := make([]reconcile.Request, len(configList.Items))
	for i := range configList.Items {
		reqs[i] = reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&configList.Items[i]),
		}
	}
	return reqs
}

// ── Helpers ────────────────────────────────────────────────────────────────────

func isOnlyFinalizer(obj client.Object, ourFinalizer string) bool {
	f := obj.GetFinalizers()
	return len(f) == 1 && f[0] == ourFinalizer
}

// splitRefKey splits a "secretName/keyName" composite key back into its parts.
func splitRefKey(refKey string) (secretName, keyName string, err error) {
	parts := strings.SplitN(refKey, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid refKey %q: expected secretName/keyName", refKey)
	}
	return parts[0], parts[1], nil
}

// configReferencesSecret returns true when any blob contains a secret::name::*
// reference for the given secretName. Used for stale-label detection.
func configReferencesSecret(blobs []configv1alpha1.ConfigBlob, secretName string) bool {
	for _, blob := range blobs {
		var tree interface{}
		if err := json.Unmarshal(blob.Value.Raw, &tree); err != nil {
			continue
		}
		if treeReferencesSecret(tree, secretName) {
			return true
		}
	}
	return false
}

func treeReferencesSecret(node interface{}, secretName string) bool {
	switch v := node.(type) {
	case string:
		if m := secretRefPattern.FindStringSubmatch(v); m != nil {
			return m[1] == secretName
		}
	case map[string]interface{}:
		for _, child := range v {
			if treeReferencesSecret(child, secretName) {
				return true
			}
		}
	case []interface{}:
		for _, child := range v {
			if treeReferencesSecret(child, secretName) {
				return true
			}
		}
	}
	return false
}

func sha256hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// ── Secret ref parsing + substitution ─────────────────────────────────────────

// SecretRef is a parsed secret::name::key reference.
type SecretRef struct {
	SecretName string
	SecretKey  string
}

func parseSecretRefs(blobs []configv1alpha1.ConfigBlob) []SecretRef {
	seen := map[string]struct{}{}
	var refs []SecretRef
	for _, blob := range blobs {
		var tree interface{}
		if err := json.Unmarshal(blob.Value.Raw, &tree); err != nil {
			continue
		}
		walkForRefs(tree, &refs, seen)
	}
	return refs
}

func walkForRefs(node interface{}, refs *[]SecretRef, seen map[string]struct{}) {
	switch v := node.(type) {
	case string:
		if m := secretRefPattern.FindStringSubmatch(v); m != nil {
			key := m[1] + "/" + m[2]
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				*refs = append(*refs, SecretRef{SecretName: m[1], SecretKey: m[2]})
			}
		}
	case map[string]interface{}:
		for _, child := range v {
			walkForRefs(child, refs, seen)
		}
	case []interface{}:
		for _, child := range v {
			walkForRefs(child, refs, seen)
		}
	}
}

func substituteRefs(blobs []configv1alpha1.ConfigBlob, values map[string]string) ([]configv1alpha1.ConfigBlob, error) {
	result := make([]configv1alpha1.ConfigBlob, len(blobs))
	for i, blob := range blobs {
		var tree interface{}
		if err := json.Unmarshal(blob.Value.Raw, &tree); err != nil {
			return nil, fmt.Errorf("blob[%d] unmarshal: %w", i, err)
		}
		substituteInTree(tree, values)
		raw, err := json.Marshal(tree)
		if err != nil {
			return nil, fmt.Errorf("blob[%d] marshal: %w", i, err)
		}
		result[i] = configv1alpha1.ConfigBlob{
			Path:  blob.Path,
			Value: runtime.RawExtension{Raw: raw},
		}
	}
	return result, nil
}

func substituteInTree(node interface{}, values map[string]string) {
	switch v := node.(type) {
	case map[string]interface{}:
		for k, child := range v {
			if s, ok := child.(string); ok {
				if m := secretRefPattern.FindStringSubmatch(s); m != nil {
					if resolved, ok := values[m[1]+"/"+m[2]]; ok {
						v[k] = resolved
					}
				}
			} else {
				substituteInTree(child, values)
			}
		}
	case []interface{}:
		for _, child := range v {
			substituteInTree(child, values)
		}
	}
}
