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
	"testing"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
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
