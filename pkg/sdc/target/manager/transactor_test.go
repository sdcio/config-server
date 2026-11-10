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

package targetmanager

import (
	"context"
	"testing"

	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	internalconfig "github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	configv1alpha1apply "github.com/sdcio/config-server/pkg/generated/applyconfiguration/config/v1alpha1"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// These tests exercise the status a failed transaction builds
// (failedResult -> applyTo -> ReadyFromApplied) and the internal
// Config.IsRecoverable(ctx) the transaction classification relies on.
func newConfigWithPriorSuccess(name string) *configv1alpha1.Config {
	cfg := &configv1alpha1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  "default",
			Generation: 10,
		},
	}
	cfg.SetConditions(configv1alpha1.ConfigReady(10, ""))
	cfg.SetConditions(cfg.ReadyFromApplied())
	return cfg
}

func TestFailedResult_NonRecoverable(t *testing.T) {
	cfg := newConfigWithPriorSuccess("cfg-nonrecoverable")
	ctx := context.Background()

	failedResult(ctx, toInternalConfig(t, cfg), "apply failed: malformed intent", nil, false).applyTo(cfg)
	cfg.SetConditions(cfg.ReadyFromApplied())

	applied := cfg.GetCondition(configv1alpha1.ConditionTypeConfigReady)
	assert.Equal(t, metav1.ConditionFalse, applied.Status)
	assert.Equal(t, string(condv1alpha1.ConditionReasonUnrecoverable), applied.Reason)
	assert.Equal(t, int64(10), applied.ObservedGeneration)
	assert.Equal(t, ptr.To[int64](0), cfg.Status.DeviationGeneration)

	ready := cfg.GetCondition(condv1alpha1.ConditionTypeReady)
	assert.Equal(t, metav1.ConditionFalse, ready.Status, "Ready must not stay True after the failure")
	assert.Equal(t, string(condv1alpha1.ConditionReasonUnrecoverable), ready.Reason)
	assert.Equal(t, "apply failed: malformed intent", ready.Message)

	internalCfg := toInternalConfig(t, cfg)
	assert.False(t, internalCfg.IsRecoverable(ctx), "same generation: not recoverable")

	// a spec edit bumps the generation: recoverable again
	internalCfg.Generation = 11
	assert.True(t, internalCfg.IsRecoverable(ctx), "new generation: recoverable")
}

func TestFailedResult_Recoverable(t *testing.T) {
	cfg := newConfigWithPriorSuccess("cfg-recoverable")
	ctx := context.Background()

	failedResult(ctx, toInternalConfig(t, cfg), "apply failed: transient EOF", nil, true).applyTo(cfg)
	cfg.SetConditions(cfg.ReadyFromApplied())

	ready := cfg.GetCondition(condv1alpha1.ConditionTypeReady)
	assert.Equal(t, metav1.ConditionFalse, ready.Status)
	assert.Equal(t, string(condv1alpha1.ConditionReasonFailed), ready.Reason)
	assert.Nil(t, cfg.Status.DeviationGeneration)

	internalCfg := toInternalConfig(t, cfg)
	assert.True(t, internalCfg.IsRecoverable(ctx))
	assert.False(t, internalCfg.IsConfigConditionReady(), "a failed config is retried")
}

// A target outage only changes Ready: the transaction state survives, so an
// unrecoverable config stays out of the next transaction.
func TestTargetNotReady_KeepsUnrecoverable(t *testing.T) {
	cfg := newConfigWithPriorSuccess("cfg-target-down")
	cfg.SetConditions(configv1alpha1.ConfigFailedUnrecoverable(cfg.GetGeneration(), "malformed intent"))
	cfg.SetConditions(configv1alpha1.ConfigNotReady(configv1alpha1.ConfigReasonTargetNotReady, "Recovering: recovering configs"))

	assert.Equal(t, string(configv1alpha1.ConfigReasonTargetNotReady),
		cfg.GetCondition(condv1alpha1.ConditionTypeReady).Reason)
	assert.False(t, toInternalConfig(t, cfg).IsRecoverable(context.Background()))
}

// toInternalConfig converts a versioned configv1alpha1.Config to the internal
// config.Config type, mirroring the conversion the apiserver performs before
// getConfigsToTransact operates on it, so tests exercise the exact
// Config.IsRecoverable(ctx) implementation the transactor relies on.
func toInternalConfig(t *testing.T, in *configv1alpha1.Config) *internalconfig.Config {
	t.Helper()
	out := &internalconfig.Config{}
	require.NoError(t, configv1alpha1.Convert_v1alpha1_Config_To_config_Config(in, out, nil))
	return out
}

// fakeDataServer models a target where secondary needs a leaf that base sets.
type fakeDataServer struct {
	dsclient.Client
	calls []sets.Set[string]
	onSet func()
}

func (f *fakeDataServer) TransactionSet(_ context.Context, req *sdcpb.TransactionSetRequest, _ ...grpc.CallOption) (*sdcpb.TransactionSetResponse, error) {
	if f.onSet != nil {
		f.onSet()
	}
	intents := sets.New[string]()
	for _, i := range req.GetIntents() {
		intents.Insert(i.GetIntent())
	}
	f.calls = append(f.calls, intents)
	if intents.Has("default.secondary") && !intents.Has("default.base") {
		return &sdcpb.TransactionSetResponse{Intents: map[string]*sdcpb.TransactionSetResponseIntent{
			"default.secondary": {Errors: []string{"mandatory leaf missing"}},
		}}, nil
	}
	return &sdcpb.TransactionSetResponse{}, nil
}

func (f *fakeDataServer) TransactionConfirm(context.Context, *sdcpb.TransactionConfirmRequest, ...grpc.CallOption) (*sdcpb.TransactionConfirmResponse, error) {
	return &sdcpb.TransactionConfirmResponse{}, nil
}

func newTargetConfig(name string, priority int32) *configv1alpha1.Config {
	return &configv1alpha1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  "default",
			Generation: 1,
			Labels: map[string]string{
				internalconfig.TargetNamespaceKey: "default",
				internalconfig.TargetNameKey:      "dev1",
			},
		},
		Spec: configv1alpha1.ConfigSpec{
			Priority: priority,
			Config: []configv1alpha1.ConfigBlob{{
				Path:  "/interface[name=ethernet-1/1]",
				Value: runtime.RawExtension{Raw: []byte(`{"admin-state":"enable"}`)},
			}},
		},
	}
}

func getTargetConfig(t *testing.T, c client.Client, name string) *configv1alpha1.Config {
	t.Helper()
	cfg := &configv1alpha1.Config{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: name}, cfg))
	return cfg
}

func assertConfigApplied(t *testing.T, c client.Client, name string) {
	t.Helper()
	cfg := getTargetConfig(t, c, name)
	assert.True(t, cfg.GetCondition(configv1alpha1.ConditionTypeConfigReady).IsTrue(), "%s: ConfigReady", name)
	assert.True(t, cfg.GetCondition(condv1alpha1.ConditionTypeReady).IsTrue(), "%s: Ready", name)
	assert.NotNil(t, cfg.Status.AppliedConfig, "%s: appliedConfig", name)
	assert.Nil(t, cfg.Status.DeviationGeneration, "%s: deviationGeneration", name)
}

// newFakeConfigClient returns a fake client for Configs; list, if set,
// intercepts List.
func newFakeConfigClient(t *testing.T, list func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error, objs ...client.Object) client.WithWatch {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, configv1alpha1.AddToScheme(scheme))
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&configv1alpha1.Config{}).
		WithInterceptorFuncs(interceptor.Funcs{
			List: list,
			// the fake rejects a status apply without resourceVersion, the apiserver does not
			SubResourceApply: func(ctx context.Context, c client.Client, sub string, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
				if ac, ok := obj.(*configv1alpha1apply.ConfigApplyConfiguration); ok && ac.ResourceVersion == nil {
					current := &configv1alpha1.Config{}
					if err := c.Get(ctx, types.NamespacedName{Namespace: *ac.Namespace, Name: *ac.Name}, current); err != nil {
						return err
					}
					ac.WithResourceVersion(current.ResourceVersion)
				}
				return c.SubResource(sub).Apply(ctx, obj, opts...)
			},
		}).
		Build()
}

func newTestDatastore(ds *fakeDataServer) *DatastoreHandle {
	return &DatastoreHandle{
		DatastoreName: "default.dev1",
		Client:        ds,
		Schema:        &configv1alpha1.ConfigStatusLastKnownGoodSchema{Type: "srl", Vendor: "nokia", Version: "24.10.1"},
	}
}

// sdcio/config-server#533: secondary fails unrecoverably on its own, is
// retried together with base and succeeds. A Ready update from a cache that
// has not seen the success yet must not write the old failure back.
func TestTransact_RetriedUnrecoverableSurvivesStaleCache(t *testing.T) {
	ctx := context.Background()

	// once stale is set, every Config List returns the first snapshot taken
	stale := false
	staleLists := 0
	var snapshot *configv1alpha1.ConfigList
	c := newFakeConfigClient(t,
		func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			cl, ok := list.(*configv1alpha1.ConfigList)
			if !ok || !stale {
				return c.List(ctx, list, opts...)
			}
			staleLists++
			if snapshot == nil {
				if err := c.List(ctx, cl, opts...); err != nil {
					return err
				}
				snapshot = cl.DeepCopy()
				return nil
			}
			snapshot.DeepCopyInto(cl)
			return nil
		},
		newTargetConfig("secondary", 20))

	target := &configv1alpha1.Target{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "dev1"}}
	ds := &fakeDataServer{}
	dsctx := newTestDatastore(ds)
	tr := NewTransactor(c, "transactor")

	// secondary alone: the intent error is reported on the Config
	_, _ = tr.Transact(ctx, target, dsctx)
	require.False(t, getTargetConfig(t, c, "secondary").IsRecoverable(), "secondary must be unrecoverable")

	require.NoError(t, c.Create(ctx, newTargetConfig("base", 10)))
	stale = true

	// no conflict: a transaction result is written once, unconditionally
	retry, err := tr.Transact(ctx, target, dsctx)
	require.NoError(t, err)
	assert.False(t, retry)
	assert.Equal(t, 1, staleLists, "one List per transaction")
	require.Len(t, ds.calls, 2)
	assert.Equal(t, sets.New("default.base", "default.secondary"), ds.calls[1], "secondary is retried with base")
	assertConfigApplied(t, c, "secondary")
	assertConfigApplied(t, c, "base")

	// a later Ready-only update on the same stale cache: rejected, nothing overwritten
	err = tr.SetConfigsNotReady(ctx, target,
		configv1alpha1.ConfigNotReady(configv1alpha1.ConfigReasonTargetNotReady, "target down"))
	assert.True(t, apierrors.IsConflict(err), "stale update must conflict, got %v", err)
	assertConfigApplied(t, c, "secondary")
	assertConfigApplied(t, c, "base")
}

// The finalizer is on the Config before the dataserver has its intent, and
// other finalizers are left alone on add and remove.
func TestTransact_FinalizerBeforeTransaction(t *testing.T) {
	ctx := context.Background()
	const other = "example.com/other"

	cfg := newTargetConfig("base", 10)
	cfg.Finalizers = []string{other}
	c := newFakeConfigClient(t, nil, cfg)

	target := &configv1alpha1.Target{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "dev1"}}
	ds := &fakeDataServer{}
	ds.onSet = func() {
		assert.Contains(t, getTargetConfig(t, c, "base").Finalizers, finalizer, "finalizer before the intent")
	}
	tr := NewTransactor(c, "transactor")

	_, err := tr.Transact(ctx, target, newTestDatastore(ds))
	require.NoError(t, err)
	require.Len(t, ds.calls, 1)
	assert.ElementsMatch(t, []string{other, finalizer}, getTargetConfig(t, c, "base").Finalizers)
	assertConfigApplied(t, c, "base")

	ds.onSet = nil
	require.NoError(t, c.Delete(ctx, getTargetConfig(t, c, "base")))
	_, err = tr.Transact(ctx, target, newTestDatastore(ds))
	require.NoError(t, err)
	require.Len(t, ds.calls, 2)
	assert.Equal(t, []string{other}, getTargetConfig(t, c, "base").Finalizers)
}
