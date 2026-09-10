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

package targetconfigserver

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	configv1alpha1apply "github.com/sdcio/config-server/pkg/generated/applyconfiguration/config/v1alpha1"
	"github.com/sdcio/config-server/pkg/keyring"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	targetmanager "github.com/sdcio/config-server/pkg/sdc/target/manager"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const (
	testTargetName = "target1"
	testNamespace  = "default"
	testConfigName = "cfg1"
	testAliveName  = "alive"
	testGhostName  = "ghost"
)

// ── test doubles ─────────────────────────────────────────────────────────────

// stubDiscovery always reports the API group as available, so Reconcile's
// discovery gate never short-circuits in tests.
type stubDiscovery struct{}

func (stubDiscovery) ServerResourcesForGroupVersion(string) (*metav1.APIResourceList, error) {
	return &metav1.APIResourceList{}, nil
}

// stubDatastoreGetter stands in for *targetmanager.TargetManager so tests
// don't have to drive TargetRuntime's real async state machine to reach a
// "ready" dsctx.
type stubDatastoreGetter struct {
	handle *targetmanager.DatastoreHandle
	ok     bool
}

func (s stubDatastoreGetter) GetDatastore(_ context.Context, _ storebackend.Key) (*targetmanager.DatastoreHandle, bool) {
	return s.handle, s.ok
}

// stubDSClient is a no-op dsclient.Client — sufficient for paths that only
// check it's non-nil, since this test never reaches the transact branch.
type stubDSClient struct{}

func (stubDSClient) Start(context.Context) error { return nil }
func (stubDSClient) Stop(context.Context)        {}
func (stubDSClient) GetAddress() string          { return "" }
func (stubDSClient) IsConnectionReady() bool     { return true }
func (stubDSClient) IsConnected() bool           { return true }
func (stubDSClient) ConnState() connectivity.State {
	return connectivity.Ready
}
func (stubDSClient) WaitForStateChange(context.Context, connectivity.State) bool { return false }
func (stubDSClient) Connect()                                                    {}

func (stubDSClient) ListDataStore(context.Context, *sdcpb.ListDataStoreRequest, ...grpc.CallOption) (*sdcpb.ListDataStoreResponse, error) {
	return nil, nil
}
func (stubDSClient) GetDataStore(context.Context, *sdcpb.GetDataStoreRequest, ...grpc.CallOption) (*sdcpb.GetDataStoreResponse, error) {
	return nil, nil
}
func (stubDSClient) CreateDataStore(context.Context, *sdcpb.CreateDataStoreRequest, ...grpc.CallOption) (*sdcpb.CreateDataStoreResponse, error) {
	return nil, nil
}
func (stubDSClient) DeleteDataStore(context.Context, *sdcpb.DeleteDataStoreRequest, ...grpc.CallOption) (*sdcpb.DeleteDataStoreResponse, error) {
	return nil, nil
}
func (stubDSClient) TransactionSet(context.Context, *sdcpb.TransactionSetRequest, ...grpc.CallOption) (*sdcpb.TransactionSetResponse, error) {
	return nil, nil
}
func (stubDSClient) TransactionConfirm(context.Context, *sdcpb.TransactionConfirmRequest, ...grpc.CallOption) (*sdcpb.TransactionConfirmResponse, error) {
	return nil, nil
}
func (stubDSClient) TransactionCancel(context.Context, *sdcpb.TransactionCancelRequest, ...grpc.CallOption) (*sdcpb.TransactionCancelResponse, error) {
	return nil, nil
}
func (stubDSClient) ListIntent(context.Context, *sdcpb.ListIntentRequest, ...grpc.CallOption) (*sdcpb.ListIntentResponse, error) {
	return nil, nil
}
func (stubDSClient) GetIntent(context.Context, *sdcpb.GetIntentRequest, ...grpc.CallOption) (*sdcpb.GetIntentResponse, error) {
	return nil, nil
}
func (stubDSClient) WatchDeviations(context.Context, *sdcpb.WatchDeviationRequest, ...grpc.CallOption) (grpc.ServerStreamingClient[sdcpb.WatchDeviationResponse], error) {
	return nil, nil
}
func (stubDSClient) BlameConfig(context.Context, *sdcpb.BlameConfigRequest, ...grpc.CallOption) (*sdcpb.BlameConfigResponse, error) {
	return nil, nil
}

// scriptedDSClient overrides TransactionSet so tests can drive a successful
// or failed transact without a real data-server.
type scriptedDSClient struct {
	stubDSClient
	transactionSetErr error
}

func (s scriptedDSClient) TransactionSet(context.Context, *sdcpb.TransactionSetRequest, ...grpc.CallOption) (*sdcpb.TransactionSetResponse, error) {
	if s.transactionSetErr != nil {
		return nil, s.transactionSetErr
	}
	return &sdcpb.TransactionSetResponse{}, nil
}

// hookDSClient runs afterSet after a successful TransactionSet, so a test
// can simulate the apply-time Modify that creates a TargetSnapshot during
// the transact.
type hookDSClient struct {
	scriptedDSClient
	afterSet func(context.Context) error
}

func (s hookDSClient) TransactionSet(ctx context.Context, req *sdcpb.TransactionSetRequest, opts ...grpc.CallOption) (*sdcpb.TransactionSetResponse, error) {
	rsp, err := s.scriptedDSClient.TransactionSet(ctx, req, opts...)
	if err != nil {
		return rsp, err
	}
	if s.afterSet != nil {
		if err := s.afterSet(ctx); err != nil {
			return nil, err
		}
	}
	return rsp, nil
}

// fakeConfigStatusApply works around a controller-runtime v0.23 fake-client
// limitation where SubResourceClient.Apply for "status" always returns a
// spurious resourceVersion conflict (the fake client's internal Apply/Patch
// call-stack special-casing never matches a subresource Apply call, so it
// never defaults the missing resourceVersion the way real SSA does). It
// simulates SSA for this test's single-field-manager scenario by reading the
// current object, overwriting Status with what was proposed, and issuing a
// plain Status().Update — behaviorally equivalent here since nothing else
// writes to this Config's status concurrently.
func fakeConfigStatusApply(ctx context.Context, c client.Client, subResourceName string, obj runtime.ApplyConfiguration, _ ...client.SubResourceApplyOption) error {
	if subResourceName != "status" {
		return nil
	}
	cfgAC, ok := obj.(*configv1alpha1apply.ConfigApplyConfiguration)
	if !ok {
		return nil
	}
	data, err := json.Marshal(cfgAC)
	if err != nil {
		return err
	}
	var proposed configv1alpha1.Config
	if err := json.Unmarshal(data, &proposed); err != nil {
		return err
	}

	current := &configv1alpha1.Config{}
	if err := c.Get(ctx, client.ObjectKey{Name: *cfgAC.Name, Namespace: *cfgAC.Namespace}, current); err != nil {
		return err
	}
	current.Status = proposed.Status
	return c.Status().Update(ctx, current)
}

// ── fixture builders ─────────────────────────────────────────────────────────

func newTestKeyRing(t *testing.T, primary string) *keyring.KeyRing {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	raw, err := json.Marshal(map[string]interface{}{
		"primary": primary,
		"keys":    map[string]string{primary: base64.StdEncoding.EncodeToString(key)},
	})
	if err != nil {
		t.Fatalf("marshal keyring: %v", err)
	}
	kr, err := keyring.NewFromBytes(raw)
	if err != nil {
		t.Fatalf("NewFromBytes: %v", err)
	}
	return kr
}

func readyTarget() *configv1alpha1.Target {
	target := configv1alpha1.BuildTarget(
		metav1.ObjectMeta{Name: testTargetName, Namespace: testNamespace},
		configv1alpha1.TargetSpec{},
		configv1alpha1.TargetStatus{},
	)
	target.SetConditions(
		configv1alpha1.TargetDiscoveryReady(),
		configv1alpha1.TargetDatastoreReady(),
		configv1alpha1.TargetConnectionReady(),
		condv1alpha1.Ready(),
	)
	return target
}

// ── the test ─────────────────────────────────────────────────────────────────

// TestReconcile_NoOpReconcile_SelfHealsStaleTargetForConfigCondition is a
// regression test for the self-heal fix: a reconcile that finds nothing to
// transact (hasChanged == false) must still correct a stale
// TargetForConfigFailed condition left over from an earlier transient,
// instead of silently leaving it in place until some future reconcile
// happens to find real content to transact.
func TestReconcile_NoOpReconcile_SelfHealsStaleTargetForConfigCondition(t *testing.T) {
	ctx := context.Background()
	kr := newTestKeyRing(t, "v1")

	plaintext, err := json.Marshal([]config.ConfigBlob{})
	if err != nil {
		t.Fatalf("marshal blobs: %v", err)
	}
	plainHashBytes := sha256.Sum256(plaintext)
	plainHash := hex.EncodeToString(plainHashBytes[:])

	payload, err := kr.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	payload.PlainHash = plainHash

	scSpec := configv1alpha1.SensitiveConfigSpec{
		Priority: 10,
		Payload:  payload,
	}

	target := readyTarget()

	targetLabels := map[string]string{
		config.TargetNamespaceKey: testNamespace,
		config.TargetNameKey:      testTargetName,
	}

	cfg := configv1alpha1.BuildConfig(
		metav1.ObjectMeta{Name: testConfigName, Namespace: testNamespace, Labels: targetLabels},
		configv1alpha1.ConfigSpec{},
		configv1alpha1.ConfigStatus{},
	)
	// Simulate a prior transient: TargetForConfigFailed is stale even though
	// the target (and the config's own content) are fine.
	cfg.SetConditions(
		configv1alpha1.ConfigReady(""),
		configv1alpha1.ConfigResolverReady(""),
		configv1alpha1.TargetForConfigFailed("target not ready"),
	)
	cfg.SetOverallStatus()

	sensitiveConfig := &configv1alpha1.SensitiveConfig{
		ObjectMeta: metav1.ObjectMeta{Name: testConfigName, Namespace: testNamespace, Labels: targetLabels},
		Spec:       scSpec,
	}

	snapshot := configv1alpha1.BuildTargetSnapshot(
		metav1.ObjectMeta{Name: testTargetName, Namespace: testNamespace},
		configv1alpha1.TargetSnapshotSpec{
			Configs: map[string]configv1alpha1.SensitiveConfigSpec{
				testConfigName: scSpec,
			},
		},
	)

	sch := runtime.NewScheme()
	if err := configv1alpha1.AddToScheme(sch); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	baseClient := fake.NewClientBuilder().
		WithScheme(sch).
		WithObjects(target, cfg, sensitiveConfig, snapshot).
		WithStatusSubresource(cfg).
		Build()

	fakeClient := interceptor.NewClient(baseClient, interceptor.Funcs{
		SubResourceApply: fakeConfigStatusApply,
	})

	r := &reconciler{
		client:          fakeClient,
		discoveryClient: stubDiscovery{},
		finalizer: resource.NewAPIFinalizer(
			fakeClient,
			finalizer,
			fieldmanagerfinalizer,
			func(name, namespace string, finalizers ...string) runtime.ApplyConfiguration {
				ac := configv1alpha1apply.Target(name, namespace)
				if len(finalizers) > 0 {
					ac.WithFinalizers(finalizers...)
				}
				return ac
			},
		),
		targetMgr: stubDatastoreGetter{
			ok: true,
			handle: &targetmanager.DatastoreHandle{
				Client: stubDSClient{},
				Status: targetmanager.RuntimeStatus{
					Phase:        targetmanager.PhaseRunning,
					DSReady:      true,
					DSStoreReady: true,
					Recovered:    true,
				},
			},
		},
		transactor: targetmanager.NewTransactor(),
		cfgMgr:     targetmanager.NewConfigManager(fakeClient, "targetConfigManager"),
		keyring:    kr,
	}

	_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: testTargetName, Namespace: testNamespace}})
	assert.NoError(t, err, "Reconcile should not return an error on a no-op reconcile")

	got := &configv1alpha1.Config{}
	if err := fakeClient.Get(ctx, client.ObjectKey{Name: testConfigName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("get config: %v", err)
	}

	targetCond := got.GetCondition(condv1alpha1.ConditionType(configv1alpha1.ConditionTypeTargetForConfigReady))
	assert.Equal(t, metav1.ConditionTrue, targetCond.Status,
		"a no-op reconcile must self-heal a stale TargetForConfigFailed condition, not leave it in place")

	readyCond := got.GetCondition(condv1alpha1.ConditionTypeReady)
	assert.Equal(t, metav1.ConditionTrue, readyCond.Status,
		"overall Ready should follow once TargetForConfigReady is corrected")
}

func targetLabels() map[string]string {
	return map[string]string{
		config.TargetNamespaceKey: testNamespace,
		config.TargetNameKey:      testTargetName,
	}
}

func encryptSpec(t *testing.T, kr *keyring.KeyRing, marker string) configv1alpha1.SensitiveConfigSpec {
	t.Helper()
	plaintext, err := json.Marshal([]config.ConfigBlob{
		{Path: "/marker", Value: runtime.RawExtension{Raw: []byte(`{"v":"` + marker + `"}`)}},
	})
	if err != nil {
		t.Fatalf("marshal blobs: %v", err)
	}
	plainHashBytes := sha256.Sum256(plaintext)
	payload, err := kr.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	payload.PlainHash = hex.EncodeToString(plainHashBytes[:])
	return configv1alpha1.SensitiveConfigSpec{
		Priority: 10,
		Payload:  payload,
	}
}

func readyConfig(name string, conditions ...condv1alpha1.Condition) *configv1alpha1.Config {
	cfg := configv1alpha1.BuildConfig(
		metav1.ObjectMeta{Name: name, Namespace: testNamespace, Labels: targetLabels()},
		configv1alpha1.ConfigSpec{},
		configv1alpha1.ConfigStatus{},
	)
	if len(conditions) == 0 {
		conditions = []condv1alpha1.Condition{
			configv1alpha1.ConfigReady(""),
			configv1alpha1.ConfigResolverReady(""),
			configv1alpha1.TargetForConfigReady("target ready"),
		}
	}
	cfg.SetConditions(conditions...)
	cfg.SetOverallStatus()
	return cfg
}

func sensitiveConfig(name string, spec configv1alpha1.SensitiveConfigSpec) *configv1alpha1.SensitiveConfig {
	return &configv1alpha1.SensitiveConfig{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace, Labels: targetLabels()},
		Spec:       spec,
	}
}

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	sch := runtime.NewScheme()
	if err := configv1alpha1.AddToScheme(sch); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	return sch
}

func newTestReconciler(c client.Client, kr *keyring.KeyRing, dsClient targetmanager.DatastoreHandle) *reconciler {
	handle := dsClient
	if handle.Client == nil {
		handle.Client = stubDSClient{}
	}
	if handle.Status.Phase == "" {
		handle.Status = targetmanager.RuntimeStatus{
			Phase:        targetmanager.PhaseRunning,
			DSReady:      true,
			DSStoreReady: true,
			Recovered:    true,
		}
	}
	return &reconciler{
		client:          c,
		discoveryClient: stubDiscovery{},
		finalizer: resource.NewAPIFinalizer(
			c,
			finalizer,
			fieldmanagerfinalizer,
			func(name, namespace string, finalizers ...string) runtime.ApplyConfiguration {
				ac := configv1alpha1apply.Target(name, namespace)
				if len(finalizers) > 0 {
					ac.WithFinalizers(finalizers...)
				}
				return ac
			},
		),
		targetMgr: stubDatastoreGetter{
			ok:     true,
			handle: &handle,
		},
		transactor: targetmanager.NewTransactor(),
		cfgMgr:     targetmanager.NewConfigManager(c, "targetConfigManager"),
		keyring:    kr,
	}
}

func interceptTargetSnapshotWrite(onWrite func(ctx context.Context) error) interceptor.Funcs {
	run := func(ctx context.Context, obj client.Object) error {
		if _, ok := obj.(*configv1alpha1.TargetSnapshot); ok {
			return onWrite(ctx)
		}
		return nil
	}
	return interceptor.Funcs{
		SubResourceApply: fakeConfigStatusApply,
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			if err := run(ctx, obj); err != nil {
				return err
			}
			return c.Update(ctx, obj, opts...)
		},
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			if err := run(ctx, obj); err != nil {
				return err
			}
			return c.Patch(ctx, obj, patch, opts...)
		},
	}
}

func snapshotKey() types.NamespacedName {
	return types.NamespacedName{Name: testTargetName, Namespace: testNamespace}
}

func getSnapshot(t *testing.T, ctx context.Context, c client.Client) *configv1alpha1.TargetSnapshot {
	t.Helper()
	got := &configv1alpha1.TargetSnapshot{}
	if err := c.Get(ctx, snapshotKey(), got); err != nil {
		t.Fatalf("get snapshot: %v", err)
	}
	return got
}

// TestReconcile_SaveSnapshot_PruneOrphanDoesNotClobberConcurrentApplyWrite
// locks the backstop contract: after a successful transact, saveSnapshot
// must prune a snapshot key whose SensitiveConfig is gone, and must not
// clobber an unrelated key a concurrent apply-time write just updated.
func TestReconcile_SaveSnapshot_PruneOrphanDoesNotClobberConcurrentApplyWrite(t *testing.T) {
	ctx := context.Background()
	kr := newTestKeyRing(t, "v1")

	cfg1Old := encryptSpec(t, kr, "cfg1-old")
	cfg1New := encryptSpec(t, kr, "cfg1-new")
	aliveSC := encryptSpec(t, kr, "alive-sc")
	aliveApply := encryptSpec(t, kr, "alive-apply")
	ghostSpec := encryptSpec(t, kr, "ghost")

	schema := &configv1alpha1.ConfigStatusLastKnownGoodSchema{
		Type:    "srl",
		Vendor:  "nokia",
		Version: "24.10",
	}

	target := readyTarget()
	cfg1 := readyConfig(testConfigName)
	aliveCfg := readyConfig(testAliveName)
	snapshot := configv1alpha1.BuildTargetSnapshot(
		metav1.ObjectMeta{Name: testTargetName, Namespace: testNamespace},
		configv1alpha1.TargetSnapshotSpec{
			Configs: map[string]configv1alpha1.SensitiveConfigSpec{
				testConfigName: cfg1Old,
				testAliveName:  aliveSC,
				testGhostName:  ghostSpec,
			},
		},
	)

	baseClient := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(
			target, cfg1, aliveCfg,
			sensitiveConfig(testConfigName, cfg1New),
			sensitiveConfig(testAliveName, aliveSC),
			snapshot,
		).
		WithStatusSubresource(cfg1, aliveCfg).
		Build()

	injectApplyTimeWrite := func(ctx context.Context) error {
		current := &configv1alpha1.TargetSnapshot{}
		if err := baseClient.Get(ctx, snapshotKey(), current); err != nil {
			return err
		}
		if current.Spec.Configs == nil {
			current.Spec.Configs = map[string]configv1alpha1.SensitiveConfigSpec{}
		}
		current.Spec.Configs[testAliveName] = aliveApply
		return baseClient.Update(ctx, current)
	}

	fakeClient := interceptor.NewClient(baseClient, interceptTargetSnapshotWrite(injectApplyTimeWrite))
	r := newTestReconciler(fakeClient, kr, targetmanager.DatastoreHandle{
		Client: scriptedDSClient{},
		Schema: schema,
	})

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: snapshotKey()})
	assert.NoError(t, err, "successful transact should not fail Reconcile")

	got := getSnapshot(t, ctx, fakeClient)
	if _, stillThere := got.Spec.Configs[testGhostName]; stillThere {
		t.Fatalf("ghost key %q still in snapshot; backstop must prune keys with no SensitiveConfig", testGhostName)
	}
	alive, ok := got.Spec.Configs[testAliveName]
	if !ok {
		t.Fatalf("alive key %q missing from snapshot; backstop must not drop an unrelated apply-time write", testAliveName)
	}
	if alive.Payload.PlainHash != aliveApply.Payload.PlainHash {
		t.Fatalf("alive hash = %s, want apply-time hash %s (backstop clobbered a concurrent apply-time write)",
			alive.Payload.PlainHash, aliveApply.Payload.PlainHash)
	}
	if got.Spec.LastKnownGoodSchema == nil || *got.Spec.LastKnownGoodSchema != *schema {
		t.Fatalf("LastKnownGoodSchema = %+v, want %+v", got.Spec.LastKnownGoodSchema, schema)
	}
}

// TestReconcile_SaveSnapshot_SkippedOnFailedTransact locks that the
// backstop never runs after a failed transact: an orphan snapshot key is
// left in place, matching the existing HasErrors early-return.
func TestReconcile_SaveSnapshot_SkippedOnFailedTransact(t *testing.T) {
	ctx := context.Background()
	kr := newTestKeyRing(t, "v1")

	cfg1Old := encryptSpec(t, kr, "cfg1-old")
	cfg1New := encryptSpec(t, kr, "cfg1-new")
	ghostSpec := encryptSpec(t, kr, "ghost")

	cfg1 := readyConfig(testConfigName)
	snapshot := configv1alpha1.BuildTargetSnapshot(
		metav1.ObjectMeta{Name: testTargetName, Namespace: testNamespace},
		configv1alpha1.TargetSnapshotSpec{
			Configs: map[string]configv1alpha1.SensitiveConfigSpec{
				testConfigName: cfg1Old,
				testGhostName:  ghostSpec,
			},
		},
	)

	baseClient := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(
			readyTarget(), cfg1,
			sensitiveConfig(testConfigName, cfg1New),
			snapshot,
		).
		WithStatusSubresource(cfg1).
		Build()

	fakeClient := interceptor.NewClient(baseClient, interceptor.Funcs{SubResourceApply: fakeConfigStatusApply})
	r := newTestReconciler(fakeClient, kr, targetmanager.DatastoreHandle{
		Client: scriptedDSClient{transactionSetErr: fmt.Errorf("southbound failed")},
	})

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: snapshotKey()})
	assert.NoError(t, err, "handleError swallows the transact error; Reconcile itself returns nil")

	got := getSnapshot(t, ctx, fakeClient)
	if _, ok := got.Spec.Configs[testGhostName]; !ok {
		t.Fatalf("ghost key %q was pruned after a failed transact; saveSnapshot must not run", testGhostName)
	}
	if got.Spec.Configs[testConfigName].Payload.PlainHash != cfg1Old.Payload.PlainHash {
		t.Fatalf("cfg1 snapshot hash changed after a failed transact")
	}
}

// TestReconcile_FirstConfig_NoSnapshot_ConfigReachesReady is the acceptance
// test for issue #01 (TargetSnapshot NotFound in the SSA/PATCH path).
//
// When a Config is applied against a target that has never had a successful
// transaction — so no TargetSnapshot exists yet — the reconciler must still
// drive Config.Status.Ready to true after a successful transaction.
//
// In the real api-server this requires AllowCreateOnUpdate=false for the
// TargetSnapshot registry (pkg/registry/generic, DisableCreateOnUpdate option)
// so that a JSON merge-patch against a missing snapshot returns a clean
// NotFound rather than the confusing "update failed to construct UpdatedObject"
// error that prevents the configread.Modify gRPC handler from falling back to
// Create.  At the unit-test level we verify the reconciler path end-to-end:
// the fake client's Patch already returns NotFound for a missing object, so
// the saveSnapshot backstop silently falls through — the Config must still
// reach Ready=true via ProcessSuccess.
func TestReconcile_FirstConfig_NoSnapshot_ConfigReachesReady(t *testing.T) {
	ctx := context.Background()
	kr := newTestKeyRing(t, "v1")

	cfg1Spec := encryptSpec(t, kr, "cfg1-data")

	// A Config that has passed through the resolver reconciler (ConfigResolverReady=True,
	// SensitiveConfig exists) but has NOT yet been processed by the TargetConfig reconciler.
	// In production the resolver always sets ConfigResolverReady before TargetConfig picks
	// up the work, so this is the realistic initial state for a first-time Config.
	cfg1 := configv1alpha1.BuildConfig(
		metav1.ObjectMeta{Name: testConfigName, Namespace: testNamespace, Labels: targetLabels()},
		configv1alpha1.ConfigSpec{},
		configv1alpha1.ConfigStatus{},
	)
	cfg1.SetConditions(configv1alpha1.ConfigResolverReady(""))
	cfg1.SetOverallStatus() // Ready=False because ConfigReady and TargetForConfig are not yet set

	baseClient := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(
			readyTarget(),
			cfg1,
			sensitiveConfig(testConfigName, cfg1Spec),
			// intentionally no TargetSnapshot — target has never had a
			// successful transaction
		).
		WithStatusSubresource(cfg1).
		Build()

	fakeClient := interceptor.NewClient(baseClient, interceptor.Funcs{
		SubResourceApply: fakeConfigStatusApply,
	})
	r := newTestReconciler(fakeClient, kr, targetmanager.DatastoreHandle{
		Client: scriptedDSClient{}, // TransactionSet succeeds
	})

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: snapshotKey()})
	assert.NoError(t, err, "Reconcile must not return an error when no TargetSnapshot pre-exists")

	got := &configv1alpha1.Config{}
	if err := fakeClient.Get(ctx, client.ObjectKey{Name: testConfigName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("get Config after Reconcile: %v", err)
	}

	readyCond := got.GetCondition(condv1alpha1.ConditionTypeReady)
	assert.Equal(t, metav1.ConditionTrue, readyCond.Status,
		"Config created against a target with no pre-existing TargetSnapshot must reach Ready=true "+
			"after a successful transaction (issue #01: TargetSnapshot NotFound in SSA/PATCH path)")
}

// TestReconcile_ExistingSnapshot_ConfigReachesReady is the regression guard
// for issue #01: a Config applied against a target that ALREADY HAS a
// TargetSnapshot must still reach Status.Ready=true after a successful
// transaction.  This is the complement of
// TestReconcile_FirstConfig_NoSnapshot_ConfigReachesReady.
func TestReconcile_ExistingSnapshot_ConfigReachesReady(t *testing.T) {
	ctx := context.Background()
	kr := newTestKeyRing(t, "v1")

	cfg1Spec := encryptSpec(t, kr, "cfg1-data")

	// Same realistic initial state: resolver has run, TargetConfig has not yet.
	cfg1 := configv1alpha1.BuildConfig(
		metav1.ObjectMeta{Name: testConfigName, Namespace: testNamespace, Labels: targetLabels()},
		configv1alpha1.ConfigSpec{},
		configv1alpha1.ConfigStatus{},
	)
	cfg1.SetConditions(configv1alpha1.ConfigResolverReady(""))
	cfg1.SetOverallStatus()

	// An existing TargetSnapshot with different content — change detection will
	// include cfg1 in toUpdate.
	oldSpec := encryptSpec(t, kr, "cfg1-old")
	snapshot := configv1alpha1.BuildTargetSnapshot(
		metav1.ObjectMeta{Name: testTargetName, Namespace: testNamespace},
		configv1alpha1.TargetSnapshotSpec{
			Configs: map[string]configv1alpha1.SensitiveConfigSpec{
				testConfigName: oldSpec,
			},
		},
	)

	baseClient := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(readyTarget(), cfg1, sensitiveConfig(testConfigName, cfg1Spec), snapshot).
		WithStatusSubresource(cfg1).
		Build()

	fakeClient := interceptor.NewClient(baseClient, interceptor.Funcs{
		SubResourceApply: fakeConfigStatusApply,
	})
	r := newTestReconciler(fakeClient, kr, targetmanager.DatastoreHandle{
		Client: scriptedDSClient{},
	})

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: snapshotKey()})
	assert.NoError(t, err, "Reconcile must not return an error when a TargetSnapshot already exists")

	got := &configv1alpha1.Config{}
	if err := fakeClient.Get(ctx, client.ObjectKey{Name: testConfigName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("get Config after Reconcile: %v", err)
	}

	readyCond := got.GetCondition(condv1alpha1.ConditionTypeReady)
	assert.Equal(t, metav1.ConditionTrue, readyCond.Status,
		"Config applied against a target with a pre-existing TargetSnapshot must reach Ready=true "+
			"after a successful transaction (regression guard for issue #01)")
}

// TestReconcile_SaveSnapshot_RefreshesSchemaAfterApplyTimeCreate locks that
// the backstop still refreshes LastKnownGoodSchema when loadSnapshot saw
// no TargetSnapshot (empty resourceVersion) but apply-time Modify created
// one during this transact.
func TestReconcile_SaveSnapshot_RefreshesSchemaAfterApplyTimeCreate(t *testing.T) {
	ctx := context.Background()
	kr := newTestKeyRing(t, "v1")

	cfg1New := encryptSpec(t, kr, "cfg1-new")
	schema := &configv1alpha1.ConfigStatusLastKnownGoodSchema{
		Type:    "srl",
		Vendor:  "nokia",
		Version: "24.10",
	}

	cfg1 := readyConfig(testConfigName)
	baseClient := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(readyTarget(), cfg1, sensitiveConfig(testConfigName, cfg1New)).
		WithStatusSubresource(cfg1).
		Build()

	createApplyTimeSnapshot := func(ctx context.Context) error {
		snap := configv1alpha1.BuildTargetSnapshot(
			metav1.ObjectMeta{Name: testTargetName, Namespace: testNamespace},
			configv1alpha1.TargetSnapshotSpec{
				Configs: map[string]configv1alpha1.SensitiveConfigSpec{
					testConfigName: cfg1New,
				},
			},
		)
		return baseClient.Create(ctx, snap)
	}

	fakeClient := interceptor.NewClient(baseClient, interceptor.Funcs{SubResourceApply: fakeConfigStatusApply})
	r := newTestReconciler(fakeClient, kr, targetmanager.DatastoreHandle{
		Client: hookDSClient{afterSet: createApplyTimeSnapshot},
		Schema: schema,
	})

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: snapshotKey()})
	assert.NoError(t, err)

	got := getSnapshot(t, ctx, fakeClient)
	if _, ok := got.Spec.Configs[testConfigName]; !ok {
		t.Fatalf("cfg1 missing from snapshot created at apply time")
	}
	if got.Spec.LastKnownGoodSchema == nil || *got.Spec.LastKnownGoodSchema != *schema {
		t.Fatalf("LastKnownGoodSchema = %+v, want %+v (backstop must patch schema onto a snapshot apply-time just created)",
			got.Spec.LastKnownGoodSchema, schema)
	}
}
