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
	"sort"
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
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
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
	// varMarker is the opening token of a variable reference. A leaf value is a
	// reference only when it is EXACTLY "${vars.<name>}" (exact-match mode); a
	// value that merely contains the marker but is not an exact reference is
	// rejected (see leafResolver.resolveLeaf) rather than shipped verbatim.
	varMarker = "${vars."
	// requeue delay when secrets are unavailable — Secret watch won't help
	// if SensitiveConfig doesn't exist yet (no ref labels set)
	requeueOnResolutionFailure = 30 * time.Second
	// errors
	errGetCr        = "cannot get cr"
	errUpdateStatus = "cannot update status"
)

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
	// SensitivePaths are untouched here — rotation moves no secrets.
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

	// GetHash hashes the ConfigSpec — defined in config_helpers.go.
	// IMPORTANT: it MUST hash spec.Vars as well as spec.Config, otherwise a
	// change to a var's secretRef (different secret/key) with unchanged blobs
	// is invisible to change detection and the new secret is never picked up.
	configHash, err := cfgOrig.Spec.GetHash()
	if err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, cfgOrig, "cannot hash config", err), errUpdateStatus)
	}

	// Single pass: fetch the declared variables' secrets, then walk the blobs
	// once — substituting ${vars.<name>} placeholders and capturing the keyless
	// SensitivePaths in the same traversal. Pre-fetched secrets from detectChange
	// are reused to avoid double-fetching.
	out, err := r.resolveConfig(ctx, cfgOrig, fetched)
	if err != nil {
		// Resolution failed: a referenced variable is undefined/unresolved, an
		// interpolation was attempted (not yet supported), or encryption failed.
		// Surface a Resolver condition and preserve the last good SC; the Config
		// and Secret watches re-trigger, and the requeue is a safety net.
		_ = r.handleError(ctx, cfgOrig, "cannot resolve config", err)
		return ctrl.Result{RequeueAfter: requeueOnResolutionFailure}, nil
	}

	if err := r.save(ctx, cfgOrig, out, configHash, change); err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, cfgOrig, "cannot save SensitiveConfig", err), errUpdateStatus)
	}

	return ctrl.Result{}, errors.Wrap(r.handleSuccess(ctx, cfgOrig), errUpdateStatus)
}

// ── detectChange ──────────────────────────────────────────────────────────────

// detectChange evaluates all three criteria without early exit.
// Returns the full picture for accurate per-dimension annotations.
// Also returns pre-fetched secrets to avoid double-fetching in resolveConfig.
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

	// 2. Config hash: detects any structural change — added/removed/changed
	//    placeholders, changed paths, changed non-secret values, and (provided
	//    GetHash covers spec.Vars) any change to a variable's secretRef.
	currentConfigHash, err := cfg.Spec.GetHash()
	if err != nil {
		return result, nil, fmt.Errorf("hash config: %w", err)
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
		// Nothing to re-encrypt (no-payload entry).
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
	// PlainHash, ConfigHash, SecretKeyHashes, SensitivePaths are all unchanged —
	// only the wrapping key changed, not the content or its layout.
	newPayload.PlainHash = sc.Spec.Payload.PlainHash
	sc.Spec.Payload = newPayload

	metav1.SetMetaDataAnnotation(&sc.ObjectMeta, config.AnnotationConfigChanged, "false")
	metav1.SetMetaDataAnnotation(&sc.ObjectMeta, config.AnnotationSecretChanged, "false")
	metav1.SetMetaDataAnnotation(&sc.ObjectMeta, config.AnnotationKeyringChanged, "true")
	return r.client.Update(ctx, sc)
}

// ── Resolution (fetch + single walk) ───────────────────────────────────────────

// resolveOutput is everything a successful resolution produces.
type resolveOutput struct {
	payload         configv1alpha1.EncryptedPayload
	secretKeyHashes map[string]string // secretName/keyName → hash (change detection)
	sensitivePaths  []string          // keyless leaf paths handed to the dataserver
	secretNames     []string          // declared secret deps (for ref labels / watch)
}

// resolveConfig fetches the declared variables' secrets, walks the blobs once to
// substitute ${vars.<name>} placeholders and capture the keyless SensitivePaths,
// then encrypts the result. Pre-fetched secrets are reused.
func (r *reconciler) resolveConfig(
	ctx context.Context,
	cfg *configv1alpha1.Config,
	fetched map[string]*corev1.Secret,
) (resolveOutput, error) {
	lr, secretKeyHashes, secretNames, err := r.buildLeafResolver(ctx, cfg, fetched)
	if err != nil {
		return resolveOutput{}, err
	}

	resolvedBlobs, sensitivePaths, err := lr.substituteBlobs(cfg.Spec.Config)
	if err != nil {
		return resolveOutput{}, err
	}

	// Always encrypt the resolved blobs — even with no variables — so the
	// payload is a self-contained snapshot for crash recovery.
	data, err := json.Marshal(resolvedBlobs)
	if err != nil {
		return resolveOutput{}, err
	}
	plainHash := sha256hex(data)
	payload, err := r.keyring.Encrypt(data)
	if err != nil {
		return resolveOutput{}, err
	}
	payload.PlainHash = plainHash

	return resolveOutput{
		payload:         payload,
		secretKeyHashes: secretKeyHashes,
		sensitivePaths:  sensitivePaths,
		secretNames:     secretNames,
	}, nil
}

// buildLeafResolver fetches the Secret behind every declared secret-backed
// variable and returns a leafResolver, per-key hashes for change detection, and
// the secret names for dependency labels.
//
// A missing secret or key is recorded per variable rather than failing here: an
// unresolved variable only fails the reconcile if it is actually referenced
// (decided during the walk via resolveLeaf). This lets a declared-but-unused
// variable whose secret does not exist yet pass through. A non-NotFound API
// error is fatal — it is transient and worth a requeue.
func (r *reconciler) buildLeafResolver(
	ctx context.Context,
	cfg *configv1alpha1.Config,
	fetched map[string]*corev1.Secret,
) (*leafResolver, map[string]string, []string, error) {
	lr := &leafResolver{
		values:     map[string]string{},
		unresolved: map[string]error{},
	}
	secretKeyHashes := map[string]string{}
	var secretNames []string
	seenSecret := map[string]struct{}{}

	for i := range cfg.Spec.Vars {
		v := cfg.Spec.Vars[i]
		if v.SecretRef == nil {
			lr.unresolved[v.Name] = fmt.Errorf("variable has no secretRef")
			continue
		}
		sName, sKey := v.SecretRef.Name, v.SecretRef.Key
		if _, ok := seenSecret[sName]; !ok {
			seenSecret[sName] = struct{}{}
			secretNames = append(secretNames, sName)
		}

		secret, ok := fetched[sName]
		if !ok {
			secret = &corev1.Secret{}
			if err := r.apiReader.Get(ctx,
				types.NamespacedName{Name: sName, Namespace: cfg.Namespace}, secret); err != nil {
				if resource.IgnoreNotFound(err) == nil {
					lr.unresolved[v.Name] = fmt.Errorf(
						"secret %q not found in namespace %q — create the secret to resolve this variable",
						sName, cfg.Namespace)
					continue
				}
				return nil, nil, nil, fmt.Errorf("variable %q: fetch secret %q: %w", v.Name, sName, err)
			}
			fetched[sName] = secret
		}

		val, ok := secret.Data[sKey]
		if !ok {
			lr.unresolved[v.Name] = fmt.Errorf(
				"secret %q has no key %q — add the missing key to resolve this variable", sName, sKey)
			continue
		}

		lr.values[v.Name] = string(val)
		secretKeyHashes[sName+"/"+sKey] = sha256hex(val)
	}

	return lr, secretKeyHashes, secretNames, nil
}

// leafResolver decides, for a single string leaf, what its resolved value is and
// whether it is secret-derived. It is the seam between the exact-match engine and
// the tree walk: swapping resolveLeaf (and exactVarName) for a CEL evaluator
// later leaves the walk, path capture, and substitution plumbing untouched.
type leafResolver struct {
	values     map[string]string // varName → resolved value (successfully resolved only)
	unresolved map[string]error  // varName → why it could not be resolved
}

// resolveLeaf returns the resolved value, whether the leaf is secret-derived
// (→ sensitive path), and an error for an unresolved or invalid reference.
//
// Exact-match contract: a leaf is a reference only when it is EXACTLY
// "${vars.<name>}". A value that merely contains the marker (interpolation
// attempt, multiple placeholders, malformed) is rejected rather than shipped
// verbatim — that is the guard that keeps exact-match from silently leaking
// unresolved placeholders to the device.
func (lr *leafResolver) resolveLeaf(s string) (string, bool, error) {
	name := exactVarName(s)
	if name == "" {
		if strings.Contains(s, varMarker) {
			return "", false, fmt.Errorf(
				"value %q: only an exact %s<name>} reference is supported "+
					"(interpolation and expressions are not yet available)", s, varMarker)
		}
		return s, false, nil // plain literal
	}
	if val, ok := lr.values[name]; ok {
		return val, true, nil
	}
	if reason, ok := lr.unresolved[name]; ok {
		return "", false, fmt.Errorf("variable %q: %w", name, reason)
	}
	return "", false, fmt.Errorf("undefined variable %s%s}", varMarker, name)
}

// exactVarName returns the variable name when s is exactly "${vars.<name>}"
// (whole value, single placeholder, valid name), else "".
func exactVarName(s string) string {
	if !strings.HasPrefix(s, varMarker) || !strings.HasSuffix(s, "}") {
		return ""
	}
	name := s[len(varMarker) : len(s)-1]
	if name == "" || !isValidVarName(name) {
		return ""
	}
	return name
}

// isValidVarName matches the ConfigVar.Name CRD pattern ^[a-zA-Z0-9_.-]+$.
// It also rejects names containing '}' or '$', so "${vars.a}${vars.b}" is not
// mistaken for a single reference (it falls through to the interpolation guard).
func isValidVarName(name string) bool {
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '_' || c == '.' || c == '-':
		default:
			return false
		}
	}
	return true
}

// substituteBlobs walks each blob value once, substituting placeholders and
// capturing the unique, sorted, keyless sensitive paths.
func (lr *leafResolver) substituteBlobs(blobs []configv1alpha1.ConfigBlob) ([]configv1alpha1.ConfigBlob, []string, error) {
	out := make([]configv1alpha1.ConfigBlob, len(blobs))
	seen := map[string]struct{}{}
	var sensitivePaths []string

	for i, blob := range blobs {
		prefix, err := keylessXPath(blob.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("blob[%d] path %q: %w", i, blob.Path, err)
		}
		var tree interface{}
		if err := json.Unmarshal(blob.Value.Raw, &tree); err != nil {
			return nil, nil, fmt.Errorf("blob[%d] unmarshal: %w", i, err)
		}

		var paths []string
		newTree, err := substituteAndCapture(tree, prefix, lr, &paths)
		if err != nil {
			return nil, nil, fmt.Errorf("blob[%d]: %w", i, err)
		}
		for _, p := range paths {
			if _, ok := seen[p]; !ok {
				seen[p] = struct{}{}
				sensitivePaths = append(sensitivePaths, p)
			}
		}

		raw, err := json.Marshal(newTree)
		if err != nil {
			return nil, nil, fmt.Errorf("blob[%d] marshal: %w", i, err)
		}
		out[i] = configv1alpha1.ConfigBlob{Path: blob.Path, Value: runtime.RawExtension{Raw: raw}}
	}

	sort.Strings(sensitivePaths)
	return out, sensitivePaths, nil
}

// substituteAndCapture is the single walk: it substitutes every string leaf via
// resolveLeaf and records the keyless path of each secret-derived leaf, in one
// pass. Descending into an array contributes nothing to the path (list /
// leaf-list keys are wildcarded), the keyless form the dataserver requires.
func substituteAndCapture(node interface{}, path string, lr *leafResolver, paths *[]string) (interface{}, error) {
	switch v := node.(type) {
	case string:
		newVal, sensitive, err := lr.resolveLeaf(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", pathLabel(path), err)
		}
		if sensitive && path != "" {
			*paths = append(*paths, path)
		}
		return newVal, nil
	case map[string]interface{}:
		for k, child := range v {
			nv, err := substituteAndCapture(child, path+"/"+k, lr, paths)
			if err != nil {
				return nil, err
			}
			v[k] = nv
		}
		return v, nil
	case []interface{}:
		for i, child := range v {
			nv, err := substituteAndCapture(child, path, lr, paths) // array level = keyless wildcard
			if err != nil {
				return nil, err
			}
			v[i] = nv
		}
		return v, nil
	default:
		return node, nil
	}
}

func pathLabel(p string) string {
	if p == "" {
		return "(root)"
	}
	return p
}

// ── save ──────────────────────────────────────────────────────────────────────

func (r *reconciler) save(
	ctx context.Context,
	cfg *configv1alpha1.Config,
	out resolveOutput,
	configHash string,
	change changeResult,
) error {
	labels := map[string]string{
		config.TargetNamespaceKey: cfg.Labels[config.TargetNamespaceKey],
		config.TargetNameKey:      cfg.Labels[config.TargetNameKey],
	}
	for _, sn := range out.secretNames {
		labels[refLabelPrefix+sn] = "true"
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
			SecretKeyHashes: out.secretKeyHashes,
			Payload:         out.payload,
			SensitivePaths:  out.sensitivePaths,
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
		if configReferencesSecret(cfg, obj.GetName()) {
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

// splitRefKey splits a "secretName/keyName" composite key back into its parts.
func splitRefKey(refKey string) (secretName, keyName string, err error) {
	parts := strings.SplitN(refKey, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid refKey %q: expected secretName/keyName", refKey)
	}
	return parts[0], parts[1], nil
}

// configReferencesSecret returns true when any variable's secretRef targets the
// given Secret name. Used for stale-label detection on the Secret watch: if the
// var (and thus the dependency) was removed from the spec, the SC is no longer
// requeued for that Secret. Over-triggering (a defined-but-unreferenced var) is
// harmless — the reconcile is a no-op.
func configReferencesSecret(cfg *configv1alpha1.Config, secretName string) bool {
	for i := range cfg.Spec.Vars {
		if ref := cfg.Spec.Vars[i].SecretRef; ref != nil && ref.Name == secretName {
			return true
		}
	}
	return false
}

func sha256hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// keylessXPath parses an xpath and re-renders it without any key predicates.
// "/network-instance[name=default]/protocols" → "/network-instance/protocols".
// An empty or root path resolves to "" so leaf paths stay rooted at the blob.
func keylessXPath(p string) (string, error) {
	if p == "" || p == "/" {
		return "", nil
	}
	parsed, err := sdcpb.ParsePath(p)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, e := range parsed.GetElem() {
		b.WriteString("/")
		b.WriteString(e.GetName())
	}
	return b.String(), nil
}