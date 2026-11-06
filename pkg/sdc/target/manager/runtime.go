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
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	dsmanager "github.com/sdcio/config-server/pkg/sdc/dataserver/manager"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// The runtime writes nothing to the Target status. It is a state machine that
// reports through Status() and calls onChange whenever that status changes;
// the target datastore reconciler is the single writer of the Ready condition.

const (
	// collectorRetryInterval is how long we wait before re-attempting a
	// collector start that failed (e.g. device unreachable on the gNMI port).
	collectorRetryInterval = 10 * time.Second
	// recoveryRetryInterval is the backoff for transient recovery failures
	// (device not connected, datastore locked, backpressure, ...).
	// Non-recoverable failures are never retried automatically.
	recoveryRetryInterval = 5 * time.Second
	// cleanupTimeout bounds stopping the watcher/collector when the run loop exits.
	cleanupTimeout = 10 * time.Second
)

type TargetPhase string

const (
	PhasePending           TargetPhase = "Pending"
	PhaseWaitingForDS      TargetPhase = "WaitingForDS"
	PhaseEnsuringDatastore TargetPhase = "EnsuringDatastore"
	PhaseRecovering        TargetPhase = "Recovering"
	// PhaseRecoveryFailed: the configs could not be replayed (non-recoverable).
	// Terminal until a human intervenes: the target definition changes
	// (desired hash) or the recovery retry annotation gets a new value.
	PhaseRecoveryFailed TargetPhase = "RecoveryFailed"
	PhaseRunning        TargetPhase = "Running"
	PhaseDegraded       TargetPhase = "Degraded"
	PhaseDeleting       TargetPhase = "Deleting"
)

type TargetRuntime struct {
	key storebackend.Key

	ds         *dsmanager.DSConnManager
	client     client.Client // k8 client
	transactor *Transactor   // replays configs into a (re)created datastore

	// desired (from reconciler)
	desiredMu   sync.RWMutex
	desired     *sdcpb.CreateDataStoreRequest
	desiredRefs *configv1alpha1.TargetStatusUsedReferences
	desiredHash string
	// recoveryToken mirrors the recovery retry annotation; part of the recovery key
	recoveryToken string

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
	recoveryMsg    string // warnings returned by the last successful recovery
	recoveryErr    error  // error of the last failed recovery attempt
	// recovery latch: set on a non-recoverable failure. While failedKey equals
	// the current recoveryKey() no recovery is attempted. Deliberately NOT
	// cleared by datastore resets (DS restart, recreate): the configs are
	// still the same, so replaying them would fail the same way.
	recoveryFailedKey string
	recoveryFailedErr error

	// change notification towards the reconciler (Ready condition owner).
	// lastNotified is only touched by the run goroutine.
	onChange          func()
	lastNotified      RuntimeStatus
	lastNotifiedValid bool

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

	// lifecycle: one-shot. A runtime runs at most one loop and never runs
	// again after Stop; TargetManager creates a new runtime instead. This
	// rules out two concurrent loops by construction, without a mutex.
	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{} // closed by Stop
	done      chan struct{} // closed when the run loop has exited (or by Stop if it never started)
	wakeCh    chan struct{} // coalescing trigger
}

// NewTargetRuntime creates a runtime. onChange (may be nil) is called from the
// run loop whenever Status() changes; it must not block.
func NewTargetRuntime(key storebackend.Key, ds *dsmanager.DSConnManager, k8s client.Client, transactor *Transactor, onChange func()) *TargetRuntime {
	return &TargetRuntime{
		key:           key,
		ds:            ds,
		client:        k8s,
		transactor:    transactor,
		onChange:      onChange,
		subscriptions: NewSubscriptions(),
		phase:         PhasePending,
		stopCh:        make(chan struct{}),
		done:          make(chan struct{}),
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

// SetRecoveryRetryToken records the value of the recovery retry annotation
// (configv1alpha1.AnnotationRecoveryRetry).
// A changed value releases a latched recovery failure and wakes the runtime.
func (r *TargetRuntime) SetRecoveryRetryToken(token string) {
	r.desiredMu.Lock()
	changed := r.recoveryToken != token
	r.recoveryToken = token
	r.desiredMu.Unlock()
	if changed {
		r.wake()
	}
}

type RuntimeStatus struct {
	Phase        TargetPhase
	DSReady      bool
	DSStoreReady bool
	RunningHash  string
	Recovered    bool
	RecoveryMsg  string // warnings of the last successful recovery
	LastError    string
}

func (t *TargetRuntime) Status() RuntimeStatus {
	// phase + ds/datastore readiness + lastError
	t.actualMu.RLock()
	phase := t.phase
	dsReady := t.dsReady
	dsStoreReady := t.datastoreReady
	recovered := t.recovered
	recoveryMsg := t.recoveryMsg
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
		RecoveryMsg:  recoveryMsg,
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

// Start launches the run loop. Idempotent (called on every ApplyDesired);
// a no-op after Stop.
func (r *TargetRuntime) Start(ctx context.Context) {
	r.startOnce.Do(func() { go r.run(ctx) })
}

// Stop ends the run loop and waits up to timeout for it to exit. It returns
// nil only once the loop has exited: from then on the runtime can no longer
// create or touch the datastore. On timeout it returns an error; calling it
// again keeps waiting for the same loop.
func (r *TargetRuntime) Stop(timeout time.Duration) error {
	r.stopOnce.Do(func() { close(r.stopCh) })
	// Never started: nothing to wait for, and any later Start is a no-op.
	r.startOnce.Do(func() { close(r.done) })

	select {
	case <-r.done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("target runtime %s did not stop within %s", r.key.String(), timeout)
	}
}

func (r *TargetRuntime) wake() {
	select {
	case r.wakeCh <- struct{}{}:
	default:
	}
}

func (r *TargetRuntime) run(ctx context.Context) {
	defer close(r.done)
	dsEvents, dsCancel := r.ds.Subscribe(8)
	defer dsCancel()

	// initial wake to process current desired state
	r.wake()

	for {
		select {
		case <-r.stopCh:
			r.shutdown(ctx)
			return

		case <-ctx.Done():
			r.shutdown(ctx)
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

// shutdown stops the components on a detached, bounded context (ctx may
// already be cancelled). The datastore is NOT deleted here: on target
// deletion the reconciler deletes it synchronously after Stop has returned
// (so the deletion is retried and blocks the finalizer); on a plain shutdown
// it must survive.
func (r *TargetRuntime) shutdown(ctx context.Context) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	r.setPhase(cleanupCtx, PhaseDeleting, nil)
	r.stopRuntime(cleanupCtx)
}

func (t *TargetRuntime) reconcileOnce(ctx context.Context) {
	defer t.notifyIfChanged()

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

	// Don't regress a Running target to EnsuringDatastore on every wake
	// (subscription changes, retries, ...): GetDatastore/GetClient would
	// briefly report not-ready to the config reconcilers.
	if t.Status().Phase != PhaseRunning {
		t.setPhase(ctx, PhaseEnsuringDatastore, nil)
	}

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

	// Replay the configs into the datastore before it is handed out:
	// Running implies recovered, so config reconcilers never write into a
	// datastore that is still missing its intents.
	if !t.isRecovered() {
		// A previous non-recoverable failure for this exact target definition
		// and retry token: don't touch the datastore, wait for a human.
		if failErr, latched := t.recoveryLatched(); latched {
			t.setPhase(ctx, PhaseRecoveryFailed, failErr)
			return
		}

		t.setPhase(ctx, PhaseRecovering, nil)
		if err := t.recoverConfigs(ctx, cl); err != nil {
			var txErr *TransactionError
			if errors.As(err, &txErr) && !txErr.Recoverable {
				// The configs we accepted before cannot be replayed:
				// latch and stop. No retry is scheduled.
				t.latchRecoveryFailure(err)
				t.setPhase(ctx, PhaseRecoveryFailed, err)
				return
			}
			// transient (device not connected, locked, apiserver hiccup, ...)
			t.setPhase(ctx, PhaseRecovering, err)
			t.wakeAfter(recoveryRetryInterval)
			return
		}
	}

	// Ensure runtime components are running (after recovery, so the
	// deviation watcher does not observe the replay)
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
			// datastore vanished (or never existed): leave Running before we
			// touch it, so nobody gets a client to a datastore without intents
			t.setPhase(ctx, PhaseEnsuringDatastore, nil)
			if _, err := cl.CreateDataStore(ctx, desired); err != nil {
				return fmt.Errorf("create datastore %q: %w", name, err)
			}
			t.setRunningHash(wantHash)
			t.setDatastoreReady(true)
			t.setRecovered(false) // fresh datastore holds no intents
			return nil
		}

		// Anything else = DS unhealthy / auth / etc
		t.setDatastoreReady(false)
		return fmt.Errorf("get datastore %q: %w", name, err)
	}

	// 2) Exists. If hash differs, recreate.
	if haveHash != "" && wantHash != "" && haveHash != wantHash {
		// leave Running first: the datastore is about to disappear
		t.setPhase(ctx, PhaseEnsuringDatastore, nil)
		// stop runtime (watch streams, collector etc)
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
		t.setRecovered(false) // recreated datastore holds no intents
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
		t.wakeAfter(collectorRetryInterval)
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

// recoverConfigs replays all configs of this target into the datastore.
// Run-loop only. A plain (non-TransactionError) error is treated as
// recoverable by the caller.
func (t *TargetRuntime) recoverConfigs(ctx context.Context, cl dsclient.Client) error {
	log := log.FromContext(ctx).With("target", t.key.String())

	if t.transactor == nil {
		return fmt.Errorf("no transactor configured")
	}

	target := &configv1alpha1.Target{}
	if err := t.client.Get(ctx, t.key.NamespacedName, target); err != nil {
		return fmt.Errorf("get target: %w", err)
	}

	handle := &DatastoreHandle{
		Key:           t.key,
		DatastoreName: t.datastoreName(),
		Client:        cl,
		Schema:        t.GetSchema(),
		Status:        t.Status(),
		MarkRecovered: t.setRecovered,
	}

	log.Info("config recovery start")
	msg, err := t.transactor.RecoverConfigs(ctx, target, handle)
	if err != nil {
		log.Error("config recovery failed", "err", err)
		t.setRecoveryResult("", err)
		return err
	}
	t.setRecoveryResult(ptr.Deref(msg, ""), nil)
	log.Info("config recovery done")
	return nil
}

// wakeAfter schedules a coalesced wake of the run loop.
func (t *TargetRuntime) wakeAfter(d time.Duration) {
	time.AfterFunc(d, t.wake)
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
	if !b {
		t.recoveryMsg = ""
		t.recoveryErr = nil
	}
}

func (t *TargetRuntime) setRecoveryResult(msg string, err error) {
	t.actualMu.Lock()
	defer t.actualMu.Unlock()
	t.recovered = err == nil
	t.recoveryMsg = msg
	t.recoveryErr = err
	if err == nil {
		t.recoveryFailedKey = ""
		t.recoveryFailedErr = nil
	}
}

// recoveryKey identifies "what we tried to recover": the target definition
// (desired hash) plus the human retry token.
func (t *TargetRuntime) recoveryKey() string {
	t.desiredMu.RLock()
	defer t.desiredMu.RUnlock()
	return t.desiredHash + "|" + t.recoveryToken
}

func (t *TargetRuntime) latchRecoveryFailure(err error) {
	key := t.recoveryKey() // desiredMu before actualMu, never nested
	t.actualMu.Lock()
	defer t.actualMu.Unlock()
	t.recoveryFailedKey = key
	t.recoveryFailedErr = err
}

// recoveryLatched reports whether a non-recoverable failure blocks recovery
// for the current recovery key.
func (t *TargetRuntime) recoveryLatched() (error, bool) {
	key := t.recoveryKey()
	t.actualMu.RLock()
	defer t.actualMu.RUnlock()
	if t.recoveryFailedErr == nil || t.recoveryFailedKey != key {
		return nil, false
	}
	return t.recoveryFailedErr, true
}

func (t *TargetRuntime) isRecovered() bool {
	t.actualMu.RLock()
	defer t.actualMu.RUnlock()
	return t.recovered
}

// notifyIfChanged calls onChange when Status() differs from the last
// notification. Run-loop only.
func (t *TargetRuntime) notifyIfChanged() {
	st := t.Status()
	if t.lastNotifiedValid && t.lastNotified == st {
		return
	}
	t.lastNotified = st
	t.lastNotifiedValid = true
	if t.onChange != nil {
		t.onChange()
	}
}
