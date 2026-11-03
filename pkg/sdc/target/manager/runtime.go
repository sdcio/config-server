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
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	configv1alpha1apply "github.com/sdcio/config-server/pkg/generated/applyconfiguration/config/v1alpha1"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	dsmanager "github.com/sdcio/config-server/pkg/sdc/dataserver/manager"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	fieldManagerTargetRuntime = "TargetRuntime"
	// collectorRetryInterval is how long we wait before re-attempting a
	// collector start that failed (e.g. device unreachable on the gNMI port).
	collectorRetryInterval = 10 * time.Second
)

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
	desiredRefs *configv1alpha1.TargetStatusUsedReferences
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

	// subscriptions: written by the subscription reconciler, read by the run loop.
	// Subscriptions is internally synchronized.
	subscriptions *Subscriptions

	// watcher is only touched by the run goroutine (reconcileOnce / stopRuntime).
	watcher *DeviationWatcher

	// collector is created, started and stopped only by the run goroutine,
	// but it is READ concurrently by the metrics exporter (see Cache in
	// exports.go). It is therefore published through an atomic pointer:
	//   - stop:  Swap(nil) first (unpublish), then Stop the old collector
	//   - start: Store the new collector, then Start it
	//   - read:  Load exactly once and use that value
	collector atomic.Pointer[Collector]

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

func (r *TargetRuntime) SetDesired(req *sdcpb.CreateDataStoreRequest, refs *configv1alpha1.TargetStatusUsedReferences, hash string) {
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

// UpsertSubscription records the subscription and wakes the run loop, which
// decides whether the collector should (still) run. It never touches the
// collector directly: the run loop owns its lifecycle.
func (t *TargetRuntime) UpsertSubscription(_ context.Context, sub *invv1alpha1.Subscription) error {
	if err := t.subscriptions.AddSubscription(sub); err != nil {
		return err
	}
	t.wake()
	return nil
}

// DeleteSubscription removes the subscription and wakes the run loop.
func (t *TargetRuntime) DeleteSubscription(_ context.Context, sub *invv1alpha1.Subscription) error {
	if err := t.subscriptions.DelSubscription(sub); err != nil {
		return err
	}
	t.wake()
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
	if err := t.ensureRuntime(ctx, cl); err != nil {
		t.setPhase(ctx, PhaseDegraded, err)
		return
	}

	t.setPhase(ctx, PhaseRunning, nil)

	// Telemetry is best-effort: collector problems never change the target
	// phase, so a broken gNMI subscription cannot block config management.
	t.reconcileCollector(ctx, desired)
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
		// stop runtime first (watch streams, collector etc)
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

// ensureRuntime starts the components required for config management.
// The telemetry collector is handled separately by reconcileCollector.
func (t *TargetRuntime) ensureRuntime(ctx context.Context, cl dsclient.Client) error {
	if t.watcher == nil {
		t.watcher = NewDeviationWatcher(t.key, t.client, cl)
		t.watcher.Start(ctx)
	}
	return nil
}

// reconcileCollector converges the gNMI telemetry collector with the current
// desired target and subscription state. Run-loop only: it is the only
// writer of t.collector.
func (t *TargetRuntime) reconcileCollector(ctx context.Context, desired *sdcpb.CreateDataStoreRequest) {
	log := log.FromContext(ctx).With("target", t.key.String())

	address, reason := t.collectorAddress(desired)
	if reason != "" {
		if t.stopCollector(ctx) {
			log.Info("stopped collector", "reason", reason)
		} else {
			log.Debug("collector not started", "reason", reason)
		}
		return
	}

	c := t.collector.Load()

	// Already running against the same endpoint: just push the new path set.
	if c != nil && c.IsRunning() && c.Address() == address {
		c.NotifySubscriptionChanged()
		return
	}

	if c == nil {
		c = NewCollector(t.key, t.subscriptions)
		// publish before Start: readers get its cache, which is valid (empty)
		// until the subscription fills it
		t.collector.Store(c)
	}
	// Start stops any previous instance (e.g. the port changed).
	if err := c.Start(ctx, desired, address); err != nil {
		log.Error("collector start failed", "err", err, "retryIn", collectorRetryInterval)
		time.AfterFunc(collectorRetryInterval, t.wake)
	}
}

// stopCollector unpublishes the collector and then stops it, so concurrent
// readers never obtain a collector that is being torn down. Returns true if
// a collector was stopped.
func (t *TargetRuntime) stopCollector(ctx context.Context) bool {
	c := t.collector.Swap(nil)
	if c == nil {
		return false
	}
	c.Stop(ctx)
	return true
}

// collectorAddress returns the gNMI address to subscribe to, or a non-empty
// reason why the collector must not run.
//
// Port precedence: subscription port, then the target's port.
func (t *TargetRuntime) collectorAddress(desired *sdcpb.CreateDataStoreRequest) (string, string) {
	if desired == nil || desired.Target == nil {
		return "", "no target"
	}
	if !strings.EqualFold(desired.Target.Type, string(invv1alpha1.Protocol_GNMI)) {
		return "", fmt.Sprintf("target protocol %q is not gnmi", desired.Target.Type)
	}
	if desired.Target.Address == "" {
		return "", "target has no address"
	}
	if !t.subscriptions.HasActivePaths() {
		return "", "no active subscription paths"
	}
	port := t.subscriptions.Port()
	if port == 0 {
		port = uint(desired.Target.Port)
	}
	if port == 0 {
		return "", "no port defined on subscription or target"
	}
	return net.JoinHostPort(desired.Target.Address, strconv.FormatUint(uint64(port), 10)), ""
}

func (t *TargetRuntime) stopRuntime(ctx context.Context) {
	if t.watcher != nil {
		t.watcher.Stop(ctx)
		t.watcher = nil
	}
	t.stopCollector(ctx)
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

func (t *TargetRuntime) setDSReady(_ context.Context, ready bool, err error) {
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

func (t *TargetRuntime) setPhase(_ context.Context, p TargetPhase, err error) {
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
	if r.lastConnValid && r.lastConn == connected && time.Since(r.lastPushed) < 2*time.Second {
		r.statusMu.Unlock()
		return
	}
	//we're about to push, but don't mark as "done" yet unless apply succeeds
	pushing := connected
	pushingMsg := msg
	r.statusMu.Unlock()

	target := &configv1alpha1.Target{}
	if err := r.client.Get(ctx, r.key.NamespacedName, target); err != nil {
		log.Error("target fetch failed", "error", err)
		return
	}

	var newCond condv1alpha1.Condition
	if connected {
		newCond = configv1alpha1.TargetConnectionReady()
	} else {
		newCond = configv1alpha1.TargetConnectionFailed(msg)
	}

	oldCond := target.GetCondition(configv1alpha1.ConditionTypeTargetConnectionReady)
	if newCond.Equal(oldCond) {
		log.Info("pushConnIfChanged -> no change",
			"connected", connected,
			"phase", r.Status().Phase,
			"msg", msg,
		)
		// Already correct in storage — mark as pushed so we don't retry needlessly
		r.statusMu.Lock()
		r.lastConnValid = true
		r.lastConn = pushing
		r.lastPushed = time.Now()
		r.statusMu.Unlock()
		return
	}

	log.Info("pushConnIfChanged -> applying",
		"connected", pushing,
		"phase", r.Status().Phase,
		"msg", pushingMsg,
	)

	applyConfig := configv1alpha1apply.Target(target.Name, target.Namespace).
		WithStatus(configv1alpha1apply.TargetStatus().
			WithConditions(newCond),
		)

	if err := r.client.Status().Apply(ctx, applyConfig, &client.SubResourceApplyOptions{
		ApplyOptions: client.ApplyOptions{
			FieldManager: fieldManagerTargetRuntime,
		},
	}); err != nil {
		log.Error("target status patch failed", "error", err)
		return
	}

	// Only mark as successfully pushed AFTER Apply succeeds
	r.statusMu.Lock()
	r.lastConnValid = true
	r.lastConn = pushing
	r.lastPushed = time.Now()
	r.statusMu.Unlock()

	log.Info("pushConnIfChanged -> updated",
		"connected", connected,
		"phase", r.Status().Phase,
		"msg", msg,
	)
}