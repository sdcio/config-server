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
	"fmt"
	"strings"
	"sync"
	"time"

	//"github.com/henderiw/logger/log"
	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	dsmanager "github.com/sdcio/config-server/pkg/sdc/dataserver/manager"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const fieldManagerTargetRuntime = "TargetRuntime"

type TargetPhase string

const (
	PhasePending           TargetPhase = "Pending"
	PhaseWaitingForDS      TargetPhase = "WaitingForDS"
	PhaseEnsuringDatastore TargetPhase = "EnsuringDatastore"
	PhaseRunning           TargetPhase = "Running"
	PhaseDegraded          TargetPhase = "Degraded"
	PhaseDeleting          TargetPhase = "Deleting"
)

type TargetRuntime struct {
	key storebackend.Key

	ds     *dsmanager.DSConnManager
	client client.Client // k8 client

	// desired (from reconciler)
	desiredMu   sync.RWMutex
	desired     *sdcpb.CreateDataStoreRequest
	desiredRefs *invv1alpha1.TargetStatusUsedReferences
	desiredHash string

	// running state
	runningMu   sync.RWMutex
	runningHash string

	// actual/runtime state (from runtime)
	actualMu       sync.RWMutex
	phase          TargetPhase
	lastError      error
	dsReady        bool
	datastoreReady bool
	recovered      bool

	// sync connection status
	statusMu      sync.Mutex
	lastPushed    time.Time
	lastConnValid bool
	lastConn      bool

	// subscriptions
	subscriptions *Subscriptions

	// owned runtime components (only runtime starts/stops them)
	watcher   *DeviationWatcher
	collector *Collector

	// control
	runMu  sync.Mutex
	cancel context.CancelFunc
	wakeCh chan struct{} // coalescing trigger
}

func NewTargetRuntime(key storebackend.Key, ds *dsmanager.DSConnManager, k8s client.Client) *TargetRuntime {
	return &TargetRuntime{
		key:           key,
		ds:            ds,
		client:        k8s,
		subscriptions: NewSubscriptions(),
		phase:         PhasePending,
		wakeCh:        make(chan struct{}, 1),
	}
}

func (r *TargetRuntime) SetDesired(req *sdcpb.CreateDataStoreRequest, refs *invv1alpha1.TargetStatusUsedReferences, hash string) {
	r.desiredMu.Lock()
	r.desired = req
	r.desiredRefs = refs
	r.desiredHash = hash
	r.desiredMu.Unlock()
	r.wake()
}

type RuntimeStatus struct {
	Phase        TargetPhase
	DSReady      bool
	DSStoreReady bool
	RunningHash  string
	Recovered    bool
	LastError    string
}

func (t *TargetRuntime) Status() RuntimeStatus {
	// phase + ds/datastore readiness + lastError
	t.actualMu.RLock()
	phase := t.phase
	dsReady := t.dsReady
	dsStoreReady := t.datastoreReady
	recovered := t.recovered
	err := t.lastError
	t.actualMu.RUnlock()

	// runningHash
	t.runningMu.RLock()
	rh := t.runningHash
	t.runningMu.RUnlock()

	s := RuntimeStatus{
		Phase:        phase,
		DSReady:      dsReady,
		DSStoreReady: dsStoreReady,
		RunningHash:  rh,
		Recovered:    recovered,
	}
	if err != nil {
		s.LastError = err.Error()
	}
	return s
}

func (t *TargetRuntime) GetSchema() *configv1alpha1.ConfigStatusLastKnownGoodSchema {
	t.desiredMu.RLock()
	defer t.desiredMu.RUnlock()

	if t.desired == nil || t.desired.Schema == nil {
		return &configv1alpha1.ConfigStatusLastKnownGoodSchema{}
	}

	return &configv1alpha1.ConfigStatusLastKnownGoodSchema{
		Type:    t.desired.Schema.Name,
		Vendor:  t.desired.Schema.Vendor,
		Version: t.desired.Schema.Version,
	}
}

func (t *TargetRuntime) UpsertSubscription(ctx context.Context, sub *invv1alpha1.Subscription) error {
	// update internal state
	t.ensureCollector()
	t.collector.SetPort(uint(sub.Spec.Port))

	if err := t.subscriptions.AddSubscription(sub); err != nil {
		return err
	}

	// if runtime is running and collector not running -> start
	st := t.Status()
	if st.Phase == PhaseRunning && t.collector != nil && !t.collector.IsRunning() {
		req := t.getDesired()
		if req != nil && req.Target != nil {
			return t.collector.Start(ctx, req)
		}
	}

	// otherwise just notify update
	if t.collector != nil && t.collector.IsRunning() {
		t.collector.NotifySubscriptionChanged()
	}
	return nil
}

func (t *TargetRuntime) DeleteSubscription(ctx context.Context, sub *invv1alpha1.Subscription) error {
	if t.subscriptions == nil {
		return nil
	}
	if err := t.subscriptions.DelSubscription(sub); err != nil {
		return err
	}

	if t.collector == nil || !t.collector.IsRunning() {
		return nil
	}

	if !t.subscriptions.HasSubscriptions() {
		t.collector.Stop(ctx)
		return nil
	}

	t.collector.NotifySubscriptionChanged()
	return nil
}

func (r *TargetRuntime) Start(ctx context.Context) {
	r.runMu.Lock()
	defer r.runMu.Unlock()

	if r.cancel != nil {
		return // already running
	}
	runCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	if r.wakeCh == nil {
		r.wakeCh = make(chan struct{}, 1)
	}
	go r.run(runCtx)
}

func (r *TargetRuntime) Stop() {
	r.runMu.Lock()
	cancel := r.cancel
	r.cancel = nil
	r.runMu.Unlock()

	if cancel != nil {
		cancel()
	}
}

func (r *TargetRuntime) wake() {
	if r.wakeCh == nil {
		return
	}
	select {
	case r.wakeCh <- struct{}{}:
	default:
	}
}

func (r *TargetRuntime) run(ctx context.Context) {
	dsEvents, dsCancel := r.ds.Subscribe(8)
	defer dsCancel()

	// initial wake to process current desired state
	r.wake()

	for {
		select {
		case <-ctx.Done():
			r.setPhase(ctx, PhaseDeleting, ctx.Err())
			r.stopRuntime(ctx)
			return

		case ev, ok := <-dsEvents:
			if !ok {
				r.setDSReady(ctx, false, fmt.Errorf("ds events closed"))
				r.stopRuntime(ctx)
				continue
			}
			r.setDSReady(ctx, ev.State == dsmanager.DSUp, ev.Err)
			r.wake()

		case <-r.wakeCh:
			r.reconcileOnce(ctx)
		}
	}
}

func (t *TargetRuntime) reconcileOnce(ctx context.Context) {
	defer func() {
		c, msg := t.connSnapshot()
		t.pushConnIfChanged(ctx, c, msg)
	}()

	desired := t.getDesired()
	if desired == nil {
		// Nothing desired => stop runtime
		t.stopRuntime(ctx)
		t.ensureDatastoreDeleted(ctx)
		t.setRecovered(false)
		if t.getDesired() == nil {
			// Only mark Pending if we're not already degraded
			st := t.Status()
			if st.Phase != PhaseDegraded {
				t.setPhase(ctx, PhasePending, nil)
			}
		}
		return
	}

	if !t.isDSReady() {
		t.setPhase(ctx, PhaseWaitingForDS, t.getLastErr())
		t.stopRuntime(ctx)
		return
	}

	t.setPhase(ctx, PhaseEnsuringDatastore, nil)

	// Get DS client (should be Ready already because DS manager blocks Start->Ready)
	cl := t.ds.Client()
	if cl == nil {
		// Race: ds transitioned down; next loop will fix.
		t.setPhase(ctx, PhaseWaitingForDS, fmt.Errorf("dataserver client nil"))
		return
	}

	// Ensure datastore exists + matches desired hash
	if err := t.ensureDatastore(ctx, cl, desired); err != nil {
		t.setPhase(ctx, PhaseDegraded, err)
		return
	}

	// Ensure runtime components are running
	if err := t.ensureRuntime(ctx, cl, desired); err != nil {
		t.setPhase(ctx, PhaseDegraded, err)
		return
	}

	t.setPhase(ctx, PhaseRunning, nil)
}

func (t *TargetRuntime) ensureDatastore(ctx context.Context, cl dsclient.Client, desired *sdcpb.CreateDataStoreRequest) error {
	name := t.datastoreName()

	// If desired hash changed from what runtime is using: force recreation.
	wantHash := t.getDesiredHash()
	haveHash := t.getRunningHash()

	// 1) Probe datastore
	_, err := cl.GetDataStore(ctx, &sdcpb.GetDataStoreRequest{DatastoreName: name})
	if err != nil {
		// Treat "not found" as create.
		if isUnknownDatastoreErr(err) {
			if _, err := cl.CreateDataStore(ctx, desired); err != nil {
				return fmt.Errorf("create datastore %q: %w", name, err)
			}
			t.setRunningHash(wantHash)
			t.setDatastoreReady(true)
			return nil
		}

		// Anything else = DS unhealthy / auth / etc
		t.setDatastoreReady(false)
		return fmt.Errorf("get datastore %q: %w", name, err)
	}

	// 2) Exists. If hash differs, recreate.
	if haveHash != "" && wantHash != "" && haveHash != wantHash {
		// stop runtime first (watch streams etc)
		t.stopRuntime(ctx)

		// delete datastore
		if _, err := cl.DeleteDataStore(ctx, &sdcpb.DeleteDataStoreRequest{Name: name}); err != nil {
			if !isUnknownDatastoreErr(err) { // if it vanished concurrently, fine
				return fmt.Errorf("delete datastore %q: %w", name, err)
			}
		}

		// recreate
		if _, err := cl.CreateDataStore(ctx, desired); err != nil {
			return fmt.Errorf("recreate datastore %q: %w", name, err)
		}

		t.setRunningHash(wantHash)
		t.setDatastoreReady(true)
		return nil
	}

	// 3) Exists and hash matches (or no hash yet) => OK
	if haveHash == "" {
		t.setRunningHash(wantHash) // first time we observed it exists
	}
	t.setDatastoreReady(true)
	return nil
}

func isUnknownDatastoreErr(err error) bool {
	if err == nil {
		return false
	}

	// gRPC status codes (preferred if DS uses them)
	if st, ok := status.FromError(err); ok {
		if st.Code() == codes.NotFound {
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unknown datastore") || strings.Contains(msg, "not found")
}

func (t *TargetRuntime) ensureDatastoreDeleted(ctx context.Context) {
	// If DS isn't ready, we can't delete right now. We'll try again on DS-up events.
	if !t.isDSReady() {
		t.setDatastoreReady(false)
		t.setRunningHash("") // we no longer "own" any running config
		return
	}

	cl := t.ds.Client()
	if cl == nil {
		t.setDatastoreReady(false)
		t.setRunningHash("")
		return
	}

	name := t.datastoreName()

	// Best-effort delete; NotFound is OK.
	if _, err := cl.DeleteDataStore(ctx, &sdcpb.DeleteDataStoreRequest{Name: name}); err != nil {
		if isUnknownDatastoreErr(err) {
			// already gone
			t.setDatastoreReady(false)
			t.setRunningHash("")
			return
		}
		// DS is up but deletion failed => surface degraded-ish error,
		// but keep desired=nil semantics (we still want it gone).
		t.setDatastoreReady(false)
		t.setPhase(ctx, PhaseDegraded, fmt.Errorf("delete datastore %q: %w", name, err))
		return
	}

	t.setDatastoreReady(false)
	t.setRunningHash("")
}

func (t *TargetRuntime) ensureRuntime(ctx context.Context, cl dsclient.Client, desired *sdcpb.CreateDataStoreRequest) error {
	if t.watcher == nil {
		t.watcher = NewDeviationWatcher(t.key, t.client, cl)
		t.watcher.Start(ctx)
	}
	if t.collector == nil {
		t.collector = NewCollector(t.key, t.subscriptions)
		if err := t.collector.Start(ctx, desired); err != nil {
			return err
		}
	}
	return nil
}

func (t *TargetRuntime) stopRuntime(ctx context.Context) {
	if t.watcher != nil {
		t.watcher.Stop(ctx)
		t.watcher = nil
	}
	if t.collector != nil {
		t.collector.Stop(ctx)
		t.collector = nil
	}
}

func (t *TargetRuntime) ensureCollector() {
	if t.collector == nil {
		t.collector = NewCollector(t.key, t.subscriptions)
	}
}

func (t *TargetRuntime) datastoreName() string {
	return t.key.String()
}

func (t *TargetRuntime) getDesiredHash() string {
	t.desiredMu.RLock()
	defer t.desiredMu.RUnlock()
	return t.desiredHash
}

func (t *TargetRuntime) getRunningHash() string {
	t.runningMu.RLock()
	defer t.runningMu.RUnlock()
	return t.runningHash
}

func (t *TargetRuntime) setRunningHash(h string) {
	t.runningMu.Lock()
	defer t.runningMu.Unlock()
	t.runningHash = h

}

func (t *TargetRuntime) setDatastoreReady(ready bool) {
	t.actualMu.Lock()
	defer t.actualMu.Unlock()
	t.datastoreReady = ready
	if !ready {
		t.recovered = false
	}
}

func (t *TargetRuntime) getDesired() *sdcpb.CreateDataStoreRequest {
	t.desiredMu.RLock()
	defer t.desiredMu.RUnlock()
	return t.desired
}

func (t *TargetRuntime) isDSReady() bool {
	t.actualMu.RLock()
	defer t.actualMu.RUnlock()
	return t.dsReady
}

func (t *TargetRuntime) getLastErr() error {
	t.actualMu.RLock()
	defer t.actualMu.RUnlock()
	return t.lastError
}

func (t *TargetRuntime) setDSReady(_ctx context.Context, ready bool, err error) {
	t.actualMu.Lock()
	defer t.actualMu.Unlock()
	t.dsReady = ready
	if ready {
		t.lastError = nil
	} else {
		t.recovered = false
		if err != nil {
			t.lastError = err
		}
	}
}

func (t *TargetRuntime) setPhase(_ctx context.Context, p TargetPhase, err error) {
	t.actualMu.Lock()
	defer t.actualMu.Unlock()
	t.phase = p
	t.lastError = err
}

func (t *TargetRuntime) setRecovered(b bool) {
	t.actualMu.Lock()
	defer t.actualMu.Unlock()
	t.recovered = b
}

func (t *TargetRuntime) connSnapshot() (bool, string) {
	st := t.Status()

	if st.Phase == PhaseRunning && st.DSReady && st.DSStoreReady {
		return true, ""
	}

	// pick a helpful message
	if !st.DSReady {
		if st.LastError != "" {
			return false, "dataserver not ready: " + st.LastError
		}
		return false, "dataserver not ready"
	}
	if !st.DSStoreReady {
		if st.LastError != "" {
			return false, "datastore not ready: " + st.LastError
		}
		return false, "datastore not ready"
	}
	if st.Phase == PhaseDegraded && st.LastError != "" {
		return false, "degraded: " + st.LastError
	}
	if st.Phase == PhaseWaitingForDS {
		return false, "waiting for dataserver"
	}
	if st.Phase == PhaseEnsuringDatastore {
		return false, "ensuring datastore"
	}
	if st.Phase == PhasePending {
		return false, "pending"
	}
	return false, string(st.Phase)
}

func (r *TargetRuntime) pushConnIfChanged(ctx context.Context, connected bool, msg string) {
	log := log.FromContext(ctx)
	log.Info("pushConnIfChanged entry")

	r.statusMu.Lock()
	// throttle flaps
	if r.lastConnValid && r.lastConn == connected && time.Since(r.lastPushed) < 2*time.Second {
		r.statusMu.Unlock()
		return
	}
	r.lastPushed = time.Now()
	r.lastConnValid = true
	r.lastConn = connected
	r.statusMu.Unlock()

	// get from informer cache
	target := &invv1alpha1.Target{}
	if err := r.client.Get(ctx, r.key.NamespacedName, target); err != nil {
		log.Error("target fetch failed", "error", err)
		return
	}

	newTarget := invv1alpha1.BuildTarget(
		metav1.ObjectMeta{
			Namespace: target.Namespace,
			Name:      target.Name,
		},
		invv1alpha1.TargetSpec{},
		invv1alpha1.TargetStatus{},
	)

	// mutate only what you own in status
	// (use your existing helpers, but mutate `target`, not a separate object)
	if connected {
		newTarget.SetConditions(invv1alpha1.TargetConnectionReady())
	} else {
		newTarget.SetConditions(invv1alpha1.TargetConnectionFailed(msg))
	}

	// if no effective status change, skip write
	oldCond := target.GetCondition(invv1alpha1.ConditionTypeTargetConnectionReady)
	newCond := newTarget.GetCondition(invv1alpha1.ConditionTypeTargetConnectionReady)
	changed := !newCond.Equal(oldCond)
	if !changed {
		log.Info("pushConnIfChanged -> no change",
			"connected", connected,
			"phase", r.Status().Phase,
			"msg", msg,
		)
		return
	}

	// PATCH /status using MergeFrom
	if err := r.client.Status().Patch(ctx, target, client.Apply, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: fieldManagerTargetRuntime,
		},
	}); err != nil {
		log.Error("target status patch failed", "error", err)
		return
	}

	log.Info("pushConnIfChanged -> updated",
		"connected", connected,
		"phase", r.Status().Phase,
		"msg", msg,
	)
}
