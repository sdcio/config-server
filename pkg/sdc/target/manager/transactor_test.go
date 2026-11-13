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
	"encoding/json"
	"errors"
	"slices"
	"strings"
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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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
	cfg.SetConditions(configv1alpha1.ConfigNotReady(cfg.GetGeneration(), configv1alpha1.ConfigReasonTargetNotReady, "Recovering: recovering configs"))

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
	fail  sets.Set[string] // intents that fail with an intent error
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
	if failed := intents.Intersection(f.fail); failed.Len() > 0 {
		rsp := &sdcpb.TransactionSetResponse{Intents: map[string]*sdcpb.TransactionSetResponseIntent{}}
		for _, name := range failed.UnsortedList() {
			rsp.Intents[name] = &sdcpb.TransactionSetResponseIntent{Errors: []string{"rejected"}}
		}
		return rsp, nil
	}
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
	assert.NotNil(t, cfg.Status.AppliedConfig, "%s: appliedConfig", name)
	assert.Nil(t, cfg.Status.DeviationGeneration, "%s: deviationGeneration", name)
}

// newFakeConfigClient returns a fake client for Configs; list, if set,
// intercepts List.
//
// The generated applyconfiguration schema is empty, so the fake has no schema
// for Config and its server-side apply does not match the apiserver's (lists
// are atomic, the finalizer apply conflicts on spec). The applies Config
// takes are therefore modelled here the way the apiserver handles them:
//   - main resource: only the finalizer apply, which adds or removes our entry
//     of the finalizers set;
//   - status: per field manager, conditions are keyed on type and every other
//     status field is owned as a whole; an apply removes what the manager
//     applied before and leaves out now, a resourceVersion is a precondition
//     (409 on mismatch), and an update that changes nothing is dropped.
func newFakeConfigClient(t *testing.T, list func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error, objs ...client.Object) client.WithWatch {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, configv1alpha1.AddToScheme(scheme))
	status := newStatusApplier()
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&configv1alpha1.Config{}).
		WithInterceptorFuncs(interceptor.Funcs{
			List: list,
			Apply: func(ctx context.Context, c client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
				ac, ok := obj.(*configv1alpha1apply.ConfigApplyConfiguration)
				if !ok || ac.Spec != nil || ac.Status != nil {
					return c.Apply(ctx, obj, opts...)
				}
				current := &configv1alpha1.Config{}
				if err := c.Get(ctx, types.NamespacedName{Namespace: *ac.Namespace, Name: *ac.Name}, current); err != nil {
					return err
				}
				if slices.Contains(ac.Finalizers, finalizer) {
					controllerutil.AddFinalizer(current, finalizer)
				} else {
					controllerutil.RemoveFinalizer(current, finalizer)
				}
				return c.Update(ctx, current)
			},
			SubResourceApply: func(ctx context.Context, c client.Client, sub string, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
				ac, ok := obj.(*configv1alpha1apply.ConfigApplyConfiguration)
				if !ok || sub != "status" {
					return c.SubResource(sub).Apply(ctx, obj, opts...)
				}
				applyOpts := &client.SubResourceApplyOptions{}
				applyOpts.ApplyOpts(opts)
				return status.apply(ctx, c, ac, applyOpts.FieldManager)
			},
		}).
		Build()
}

// statusApplier models server-side apply of the Config status, see
// newFakeConfigClient.
type statusApplier struct {
	// owned is, per Config and field manager, what the manager applied:
	// "conditions/<type>" or a status field name
	owned map[types.NamespacedName]map[string]sets.Set[string]
}

func newStatusApplier() *statusApplier {
	return &statusApplier{owned: map[types.NamespacedName]map[string]sets.Set[string]{}}
}

func (a *statusApplier) apply(ctx context.Context, c client.Client, ac *configv1alpha1apply.ConfigApplyConfiguration, manager string) error {
	key := types.NamespacedName{Namespace: *ac.Namespace, Name: *ac.Name}
	current := &configv1alpha1.Config{}
	if err := c.Get(ctx, key, current); err != nil {
		return err
	}
	if ac.ResourceVersion != nil && *ac.ResourceVersion != current.ResourceVersion {
		return apierrors.NewConflict(configv1alpha1.Resource("configs"), key.Name,
			errors.New("the object has been modified"))
	}

	applied, err := toMap(ac.Status)
	if err != nil {
		return err
	}
	cur, err := toMap(current.Status)
	if err != nil {
		return err
	}
	before, err := json.Marshal(cur)
	if err != nil {
		return err
	}
	appliedConds, _ := applied["conditions"].([]any)
	curConds, _ := cur["conditions"].([]any)
	delete(applied, "conditions")

	now := sets.New[string]()
	for k := range applied {
		now.Insert(k)
	}
	for _, cond := range appliedConds {
		now.Insert("conditions/" + condType(cond))
	}

	// what the manager owned and leaves out now is removed
	if a.owned[key] == nil {
		a.owned[key] = map[string]sets.Set[string]{}
	}
	for k := range a.owned[key][manager].Difference(now) {
		if typ, ok := strings.CutPrefix(k, "conditions/"); ok {
			curConds = slices.DeleteFunc(curConds, func(c any) bool { return condType(c) == typ })
			continue
		}
		delete(cur, k)
	}
	a.owned[key][manager] = now

	for k, v := range applied {
		cur[k] = v
	}
	for _, cond := range appliedConds {
		i := slices.IndexFunc(curConds, func(c any) bool { return condType(c) == condType(cond) })
		if i < 0 {
			curConds = append(curConds, cond)
		} else {
			curConds[i] = cond
		}
	}
	cur["conditions"] = curConds

	data, err := json.Marshal(cur)
	if err != nil {
		return err
	}
	// like the apiserver: a status update that changes nothing is dropped
	if string(data) == string(before) {
		return nil
	}
	current.Status = configv1alpha1.ConfigStatus{}
	if err := json.Unmarshal(data, &current.Status); err != nil {
		return err
	}
	return c.Status().Update(ctx, current)
}

func toMap(v any) (map[string]any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	if string(data) == "null" {
		return m, nil
	}
	return m, json.Unmarshal(data, &m)
}

func condType(c any) string {
	m, _ := c.(map[string]any)
	t, _ := m["type"].(string)
	return t
}

// transact lists the Configs of the target, as the reconciler does, and
// transacts them.
func transact(t *testing.T, tr *Transactor, c client.Client, target *configv1alpha1.Target, dsctx *DatastoreHandle) (bool, error) {
	t.Helper()
	cfgs, err := ListTargetConfigs(context.Background(), c, target)
	require.NoError(t, err)
	return tr.Transact(context.Background(), target, dsctx, cfgs)
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
	tr := NewTransactor(c)

	// secondary alone: the intent error is reported on the Config
	_, _ = transact(t, tr, c, target, dsctx)
	require.False(t, getTargetConfig(t, c, "secondary").IsRecoverable(), "secondary must be unrecoverable")

	require.NoError(t, c.Create(ctx, newTargetConfig("base", 10)))
	stale = true

	// no conflict: a transaction result is written once, unconditionally
	retry, err := transact(t, tr, c, target, dsctx)
	require.NoError(t, err)
	assert.False(t, retry)
	assert.Equal(t, 1, staleLists, "one List per transaction")
	require.Len(t, ds.calls, 2)
	assert.Equal(t, sets.New("default.base", "default.secondary"), ds.calls[1], "secondary is retried with base")
	assertConfigApplied(t, c, "secondary")
	assertConfigApplied(t, c, "base")
	// Ready follows the outcome just written, not the stale list
	assert.True(t, getTargetConfig(t, c, "secondary").GetCondition(condv1alpha1.ConditionTypeReady).IsTrue(), "secondary: Ready")
	assert.True(t, getTargetConfig(t, c, "base").GetCondition(condv1alpha1.ConditionTypeReady).IsTrue(), "base: Ready")
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
	tr := NewTransactor(c)

	_, err := transact(t, tr, c, target, newTestDatastore(ds))
	require.NoError(t, err)
	require.Len(t, ds.calls, 1)
	assert.ElementsMatch(t, []string{other, finalizer}, getTargetConfig(t, c, "base").Finalizers)
	assertConfigApplied(t, c, "base")
	deviationKey := types.NamespacedName{Namespace: "default", Name: configv1alpha1.DeviationName(configv1alpha1.DeviationType_CONFIG, "base")}
	require.NoError(t, c.Get(ctx, deviationKey, &configv1alpha1.Deviation{}))

	ds.onSet = nil
	require.NoError(t, c.Delete(ctx, getTargetConfig(t, c, "base")))
	_, err = transact(t, tr, c, target, newTestDatastore(ds))
	require.NoError(t, err)
	require.Len(t, ds.calls, 2)
	assert.Equal(t, []string{other}, getTargetConfig(t, c, "base").Finalizers)
	assert.True(t, apierrors.IsNotFound(c.Get(ctx, deviationKey, &configv1alpha1.Deviation{})), "deviation deleted")
}

// A failed transaction writes ConfigReady only: appliedConfig, which recovery
// replays, survives even when the failure is written from a cache that has
// not seen the earlier success.
func TestTransact_FailureKeepsAppliedConfig(t *testing.T) {
	// once stale is set, every Config List returns the first snapshot taken:
	// the Config before its first success
	stale := false
	var snapshot *configv1alpha1.ConfigList
	c := newFakeConfigClient(t,
		func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			cl, ok := list.(*configv1alpha1.ConfigList)
			if ok && stale {
				snapshot.DeepCopyInto(cl)
				return nil
			}
			if err := c.List(ctx, list, opts...); err != nil {
				return err
			}
			if ok && snapshot == nil {
				snapshot = cl.DeepCopy()
			}
			return nil
		},
		newTargetConfig("base", 10))

	target := &configv1alpha1.Target{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "dev1"}}
	ds := &fakeDataServer{}
	dsctx := newTestDatastore(ds)
	tr := NewTransactor(c)

	_, err := transact(t, tr, c, target, dsctx)
	require.NoError(t, err)
	assertConfigApplied(t, c, "base")

	// the stale cache has not seen the success: base is transacted again and fails
	stale = true
	ds.fail = sets.New("default.base")
	_, _ = transact(t, tr, c, target, dsctx)
	require.Len(t, ds.calls, 2)

	cfg := getTargetConfig(t, c, "base")
	assert.Equal(t, string(condv1alpha1.ConditionReasonUnrecoverable),
		cfg.GetCondition(configv1alpha1.ConditionTypeConfigReady).Reason)
	assert.Equal(t, ptr.To[int64](0), cfg.Status.DeviationGeneration)
	assert.NotNil(t, cfg.Status.AppliedConfig, "appliedConfig survives the failure")
	assert.Equal(t, dsctx.Schema, cfg.Status.LastKnownGoodSchema, "lastKnownGoodSchema survives the failure")
}

// newDeletingConfig returns a Config that holds the transactor finalizer, so
// deleting it only sets the deletion timestamp.
func newDeletingConfig(name string) *configv1alpha1.Config {
	cfg := newTargetConfig(name, 10)
	cfg.Finalizers = []string{finalizer}
	return cfg
}

func deleteConfig(t *testing.T, c client.Client, name string) {
	t.Helper()
	require.NoError(t, c.Delete(context.Background(), getTargetConfig(t, c, name)))
}

func assertConfigGone(t *testing.T, c client.Client, name string) {
	t.Helper()
	err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: name}, &configv1alpha1.Config{})
	assert.True(t, apierrors.IsNotFound(err), "%s: released, got %v", name, err)
}

// Without a transaction (target not ready, ...), deleting Configs are
// released and the others are left alone (sdcio/config-server#529).
func TestReleaseDeletingConfigs(t *testing.T) {
	ctx := context.Background()
	c := newFakeConfigClient(t, nil, newDeletingConfig("gone"), newDeletingConfig("kept"))
	target := &configv1alpha1.Target{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "dev1"}}
	tr := NewTransactor(c)

	deleteConfig(t, c, "gone")
	cfgs, err := ListTargetConfigs(ctx, c, target)
	require.NoError(t, err)
	require.NoError(t, tr.ReleaseDeletingConfigs(ctx, cfgs))

	assertConfigGone(t, c, "gone")
	assert.Equal(t, []string{finalizer}, getTargetConfig(t, c, "kept").Finalizers)
}

// The delete intent does not depend on the spec: an unrecoverable Config is
// still deleted.
func TestTransact_DeletesUnrecoverable(t *testing.T) {
	cfg := newDeletingConfig("bad")
	cfg.SetConditions(configv1alpha1.ConfigFailedUnrecoverable(cfg.GetGeneration(), "malformed intent"))
	c := newFakeConfigClient(t, nil, cfg)
	target := &configv1alpha1.Target{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "dev1"}}
	ds := &fakeDataServer{}
	tr := NewTransactor(c)

	deleteConfig(t, c, "bad")
	_, err := transact(t, tr, c, target, newTestDatastore(ds))
	require.NoError(t, err)
	require.Len(t, ds.calls, 1)
	assert.Equal(t, sets.New("default.bad"), ds.calls[0])
	assertConfigGone(t, c, "bad")
}

// A deleting orphan Config is removed in a transaction of its own and
// released, even when the other intents of the target fail.
func TestTransact_OrphanDeletedOnItsOwn(t *testing.T) {
	orphan := newDeletingConfig("orphan")
	orphan.Spec.Lifecycle = &configv1alpha1.Lifecycle{DeletionPolicy: configv1alpha1.DeletionOrphan}
	c := newFakeConfigClient(t, nil, orphan, newTargetConfig("secondary", 20))
	target := &configv1alpha1.Target{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "dev1"}}
	ds := &fakeDataServer{}
	tr := NewTransactor(c)

	deleteConfig(t, c, "orphan")
	// secondary fails on its own (mandatory leaf missing)
	_, _ = transact(t, tr, c, target, newTestDatastore(ds))
	require.Len(t, ds.calls, 2)
	assert.Equal(t, sets.New("default.orphan"), ds.calls[0], "orphan in a transaction of its own")
	assert.Equal(t, sets.New("default.secondary"), ds.calls[1])
	assertConfigGone(t, c, "orphan")
}

func TestDeleteRetry(t *testing.T) {
	deleting := map[string]*internalconfig.Config{"default.customer-sr1": {}}
	nonRecoverable := errors.New("errTimeoutError: channel timeout sending input to device")

	retry, err := deleteRetry(deleting, false, nonRecoverable) // deletes: retried even if non recoverable
	assert.True(t, retry)
	assert.ErrorIs(t, err, nonRecoverable)

	retry, err = deleteRetry(deleting, false, nil) // intent errors only: still backs off
	assert.True(t, retry)
	assert.Error(t, err)

	retry, err = deleteRetry(nil, false, nonRecoverable) // no deletes: unchanged
	assert.False(t, retry)
	assert.ErrorIs(t, err, nonRecoverable)
}