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

package targetmanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	dsmanager "github.com/sdcio/config-server/pkg/sdc/dataserver/manager"
	"github.com/sdcio/config-server/pkg/sdc/target/runtimeview"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// runtimeEventBuffer sizes the runtime -> reconciler notification channel.
// Sends never block: a dropped event is covered by the reconciler's
// fallback requeue.
const runtimeEventBuffer = 1024

const (
	// runtimeStopTimeout bounds one wait for a runtime's run loop to exit.
	runtimeStopTimeout = 15 * time.Second
	// datastoreDeleteTimeout bounds the DeleteDataStore RPC during finalization.
	datastoreDeleteTimeout = 10 * time.Second
)

// DatastoreAdmin is the in-process data-server handle the manager uses for
// datastore administration. A nil handle makes the operations fail.
type DatastoreAdmin interface {
	DeleteDataStore(ctx context.Context, name string) error
}

type TargetManager struct {
	ds         *dsmanager.DSConnManager
	admin      DatastoreAdmin
	k8s        client.Client
	transactor *Transactor // shared by all runtimes for config recovery
	events     chan event.GenericEvent

	mu      sync.RWMutex
	targets map[storebackend.Key]*TargetRuntime
}

func NewTargetManager(ds *dsmanager.DSConnManager, k8s client.Client, admin DatastoreAdmin) *TargetManager {
	return &TargetManager{
		ds:         ds,
		admin:      admin,
		k8s:        k8s,
		transactor: NewTransactor(k8s),
		events:     make(chan event.GenericEvent, runtimeEventBuffer),
		targets:    make(map[storebackend.Key]*TargetRuntime),
	}
}

// Events emits a GenericEvent for a Target whenever its runtime status
// changes. The reconciler that owns the Target Ready condition watches it:
//
//	WatchesRawSource(source.Channel(targetMgr.Events(), &handler.EnqueueRequestForObject{}))
//
// Only ONE controller may consume it.
func (m *TargetManager) Events() <-chan event.GenericEvent {
	return m.events
}

func (m *TargetManager) notifier(key storebackend.Key) func() {
	return func() {
		ev := event.GenericEvent{Object: &configv1alpha1.Target{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: key.Namespace,
				Name:      key.Name,
			},
		}}
		select {
		case m.events <- ev:
		default: // full: the reconciler's fallback requeue covers it
		}
	}
}

func (m *TargetManager) GetOrCreate(key storebackend.Key) *TargetRuntime {
	m.mu.Lock()
	defer m.mu.Unlock()

	if rt, ok := m.targets[key]; ok {
		return rt
	}
	rt := NewTargetRuntime(key, m.ds, m.k8s, m.transactor, m.notifier(key))
	m.targets[key] = rt
	return rt
}

// ApplyDesired is what your reconciler calls on each reconcile.
func (m *TargetManager) ApplyDesired(
	ctx context.Context,
	key storebackend.Key,
	req *sdcpb.CreateDataStoreRequest,
	refs *configv1alpha1.TargetStatusUsedReferences,
	hash string,
) {
	rt := m.GetOrCreate(key)
	rt.Start(ctx) // idempotent
	rt.SetDesired(req, refs, hash)
}

// SetRecoveryRetryToken forwards the Target's AnnotationRecoveryRetry value to
// its runtime; a new value releases a latched recovery failure.
// Call it BEFORE ApplyDesired so the token is known before the first recovery
// attempt (otherwise an existing annotation would look like a fresh retry).
func (m *TargetManager) SetRecoveryRetryToken(_ context.Context, key storebackend.Key, token string) {
	m.GetOrCreate(key).SetRecoveryRetryToken(token)
}

// ClearDesired is useful when the Target is being deleted or disabled.
func (m *TargetManager) ClearDesired(_ context.Context, key storebackend.Key) {
	m.mu.Lock()
	rt := m.targets[key]
	m.mu.Unlock()

	if rt != nil {
		rt.SetDesired(nil, nil, "")
	}
}

// Delete stops the runtime and waits until its run loop has exited; only then
// is it removed from the manager. On timeout it returns an error and keeps the
// runtime registered, so a retry waits for the same runtime again.
//
// Delete does NOT delete the datastore: call DeleteDatastore afterwards.
func (m *TargetManager) Delete(_ context.Context, key storebackend.Key) error {
	m.mu.RLock()
	rt := m.targets[key]
	m.mu.RUnlock()

	if rt == nil {
		return nil
	}
	if err := rt.Stop(runtimeStopTimeout); err != nil {
		return err
	}

	m.mu.Lock()
	if m.targets[key] == rt {
		delete(m.targets, key)
	}
	m.mu.Unlock()
	return nil
}

// DeleteDatastore deletes the target's datastore in the data-server and
// returns nil only when it is confirmed gone (deleted or unknown).
// It needs no runtime, so it also works after a config-server restart.
// Call it after Delete, so no runtime can recreate the datastore.
func (m *TargetManager) DeleteDatastore(ctx context.Context, key storebackend.Key) error {
	if m.admin == nil {
		return fmt.Errorf("delete datastore %q: dataserver not available", key.String())
	}
	return deleteDatastore(ctx, m.admin, key.String())
}

func deleteDatastore(ctx context.Context, admin DatastoreAdmin, name string) error {
	// bounded, and independent of any (cancelled) runtime context
	ctx, cancel := context.WithTimeout(ctx, datastoreDeleteTimeout)
	defer cancel()

	if err := admin.DeleteDataStore(ctx, name); err != nil {
		if isUnknownDatastoreErr(err) {
			return nil // already gone
		}
		return fmt.Errorf("delete datastore %q: %w", name, err)
	}
	return nil
}

// GetClient returns a datastore-scoped DS client for this target,
// only if DS is ready AND datastore is ready AND configs are recovered
// (PhaseRunning implies all three).
func (m *TargetManager) GetClient(_ context.Context, key storebackend.Key) (dsclient.Client, RuntimeStatus, bool) {
	m.mu.Lock()
	rt := m.targets[key]
	m.mu.Unlock()
	if rt == nil {
		return nil, RuntimeStatus{Phase: PhasePending}, false
	}

	st := rt.Status()
	if !st.DSReady || !st.DSStoreReady || st.Phase != PhaseRunning {
		return nil, st, false
	}

	cl := m.ds.Client()
	if cl == nil {
		return nil, st, false
	}
	return cl, st, true
}

type DatastoreHandle struct {
	Key           storebackend.Key
	DatastoreName string
	Client        dsclient.Client
	Schema        *configv1alpha1.ConfigStatusLastKnownGoodSchema
	MarkRecovered func(bool) // callback into runtime
	Status        RuntimeStatus
}

// GetDatastore returns a handle for config reconcilers. It is only usable
// once the runtime is Running, which implies the configs have been recovered.
func (m *TargetManager) GetDatastore(_ context.Context, key storebackend.Key) (*DatastoreHandle, bool) {
	m.mu.Lock()
	rt := m.targets[key]
	m.mu.Unlock()
	if rt == nil {
		return nil, false
	}

	st := rt.Status()
	if !st.DSReady || !st.DSStoreReady || st.Phase != PhaseRunning {
		return &DatastoreHandle{Key: key, DatastoreName: key.String(), Status: st}, false
	}

	cl := m.ds.Client()
	if cl == nil {
		return &DatastoreHandle{Key: key, DatastoreName: key.String(), Status: st}, false
	}

	return &DatastoreHandle{
		Key:           key,
		DatastoreName: key.String(),
		Client:        cl,
		Schema:        rt.GetSchema(),
		Status:        st,
		// An external MarkRecovered(false) means "recover again": wake the
		// runtime so it re-runs recovery instead of waiting for another event.
		MarkRecovered: func(b bool) {
			rt.setRecovered(b)
			if !b {
				rt.wake()
			}
		},
	}, true
}

// GetRuntimeForTarget returns the runtime if it exists (does NOT create one).
func (m *TargetManager) GetRuntimeForTarget(_ context.Context, key storebackend.Key) *TargetRuntime {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.targets[key]
}

func ComputeCreateDSHash(req *sdcpb.CreateDataStoreRequest, refs *configv1alpha1.TargetStatusUsedReferences) string {
	h := sha256.New()

	// hash the request deterministically
	b, _ := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: true,
	}.Marshal(req)
	h.Write(b)

	// hash the used refs deterministically (stable ordering)
	if refs != nil {
		// keep it explicit so adding fields later is obvious
		h.Write([]byte("|cp=" + refs.ConnectionProfileResourceVersion))
		h.Write([]byte("|sp=" + refs.SyncProfileResourceVersion))
		h.Write([]byte("|sec=" + refs.SecretResourceVersion))
		h.Write([]byte("|tls=" + refs.TLSSecretResourceVersion))
	}

	sum := h.Sum(nil)
	return hex.EncodeToString(sum)
}

func CloneReq(req *sdcpb.CreateDataStoreRequest) *sdcpb.CreateDataStoreRequest {
	if req == nil {
		return nil
	}
	return proto.Clone(req).(*sdcpb.CreateDataStoreRequest)
}

func (m *TargetManager) listRuntimes() []*TargetRuntime {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*TargetRuntime, 0, len(m.targets))
	for _, rt := range m.targets {
		out = append(out, rt)
	}
	return out
}

func (m *TargetManager) ForEachRuntime(fn func(runtimeview.TargetRuntimeView)) {
	for _, rt := range m.listRuntimes() {
		fn(rt)
	}
}

func (m *TargetManager) ApplySubscription(ctx context.Context, sub *invv1alpha1.Subscription) ([]string, error) {
	targets, err := m.getDownstreamTargets(ctx, sub) // optional helper
	if err != nil {
		return nil, err
	}

	var errs error
	for _, tn := range targets {
		key := storebackend.KeyFromNSN(client.ObjectKey{Namespace: sub.Namespace, Name: tn})
		rt := m.GetRuntimeForTarget(ctx, key)
		if rt == nil {
			continue
		}
		if err := rt.UpsertSubscription(ctx, sub); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return targets, errs
}

func (m *TargetManager) RemoveSubscription(ctx context.Context, sub *invv1alpha1.Subscription) error {
	targets, err := m.getDownstreamTargets(ctx, sub)
	if err != nil {
		return err
	}

	var errs error
	for _, tn := range targets {
		key := storebackend.KeyFromNSN(client.ObjectKey{Namespace: sub.Namespace, Name: tn})
		rt := m.GetRuntimeForTarget(ctx, key)
		if rt == nil {
			continue
		}
		if err := rt.DeleteSubscription(ctx, sub); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}

// getDownstreamTargets list the targets
func (r *TargetManager) getDownstreamTargets(ctx context.Context, sub *invv1alpha1.Subscription) ([]string, error) {
	selector, err := metav1.LabelSelectorAsSelector(sub.Spec.Target.TargetSelector)
	if err != nil {
		return nil, fmt.Errorf("parsing selector failed: err: %s", err.Error())
	}
	opts := []client.ListOption{
		client.InNamespace(sub.Namespace),
		client.MatchingLabelsSelector{Selector: selector},
	}

	targetList := &configv1alpha1.TargetList{}
	if err := r.k8s.List(ctx, targetList, opts...); err != nil {
		return nil, err
	}
	targets := make([]string, 0, len(targetList.Items))
	for _, target := range targetList.Items {
		// only add targets that are not in deleting state
		if target.GetDeletionTimestamp().IsZero() {
			targets = append(targets, target.Name)
		}
	}
	sort.Strings(targets)
	return targets, nil
}
