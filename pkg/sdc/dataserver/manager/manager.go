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

package dataservermanager

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/henderiw/logger/log"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	"google.golang.org/grpc/connectivity"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
)

// DSState represents connectivity to the dataserver.
type DSState string

const (
	DSDown DSState = "Down"
	DSUp   DSState = "Up"
)

// Event is what subscribers receive.
type Event struct {
	State DSState
	Err   error // last error on transitions to Down (optional)
	At    time.Time
}

// DSConnManager maintains a long-lived client connection to a dataserver and
// broadcasts Up/Down transitions to subscribers.
//
// IMPORTANT: reconcilers should never create dsclient.Client directly.
// They should use this manager (or a higher-level runtime manager).
type DSConnManager struct {
	cfg *dsclient.Config

	mu         sync.RWMutex
	client     dsclient.Client
	state      DSState
	connecting bool
	cond       *sync.Cond

	subsMu sync.RWMutex
	subs   map[chan Event]struct{} // fan-out set

	// internal control
	startOnce sync.Once
	stopOnce  sync.Once
	cancel    context.CancelFunc
}

// New creates a DSConnManager for a single dataserver address/config.
func New(ctx context.Context, cfg *dsclient.Config) *DSConnManager {
	m := &DSConnManager{
		cfg:   cfg,
		state: DSDown,
		subs:  map[chan Event]struct{}{},
	}
	m.cond = sync.NewCond(&m.mu)
	return m
}

// AddToManager registers the manager as a controller-runtime Runnable.
// Call this from main during startup.
func (m *DSConnManager) AddToManager(mgr ctrl.Manager) error {
	return mgr.Add(m)
}

// Start implements controller-runtime Runnable.
func (m *DSConnManager) Start(ctx context.Context) error {
	l := log.FromContext(ctx).With("component", "dataserverManager")
	log.IntoContext(ctx, l)
	var err error
	m.startOnce.Do(func() {
		l.Info("starting")
		ctx, m.cancel = context.WithCancel(ctx)
		go m.run(ctx)
	})
	return err
}

// Stop can be used if you run it outside controller-runtime.
func (m *DSConnManager) Stop() {
	m.stopOnce.Do(func() {
		if m.cancel != nil {
			m.cancel()
			m.cancel = nil
		}
	})
}

// State returns the last known DS state.
func (m *DSConnManager) State() DSState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// Client returns the current client if Up; otherwise nil.
func (m *DSConnManager) Client() dsclient.Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.state != DSUp {
		return nil
	}
	return m.client
}

// Subscribe returns a channel that receives state transition events.
// Caller should call the returned cancel function to unsubscribe.
func (m *DSConnManager) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer <= 0 {
		buffer = 8
	}
	ch := make(chan Event, buffer)

	m.subsMu.Lock()
	m.subs[ch] = struct{}{}
	m.subsMu.Unlock()

	// Immediately send current state snapshot.
	ch <- Event{State: m.State(), At: time.Now()}

	cancel := func() {
		m.subsMu.Lock()
		if _, ok := m.subs[ch]; ok {
			delete(m.subs, ch)
			close(ch)
		}
		m.subsMu.Unlock()
	}
	return ch, cancel
}

// WaitForUp blocks until the dataserver is Up or ctx is done.
func (m *DSConnManager) WaitForUp(ctx context.Context) error {
	events, cancel := m.Subscribe(1)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-events:
			if !ok {
				return context.Canceled
			}
			if ev.State == DSUp {
				return nil
			}
		}
	}
}

// ---- internals ----

func (m *DSConnManager) run(ctx context.Context) {
	log := log.FromContext(ctx)
	// Exponential backoff for reconnect.
	backoff := wait.Backoff{
		Duration: 500 * time.Millisecond,
		Factor:   1.7,
		Jitter:   0.2,
		Steps:    0, // unlimited
		Cap:      15 * time.Second,
	}

	for {
		select {
		case <-ctx.Done():
			log.Info("stopping")
			cl := m.Client()
			m.setDown(ctx, ctx.Err())
			m.closeClient(ctx, cl)
			m.closeAllSubs()
			return
		default:
		}

		// Ensure we have a client connected & started.
		cl, err := m.ensureClient(ctx)
		if err != nil {
			m.setDown(ctx, err)
			log.Warn("connect failed", "err", err)
			m.sleepWithBackoff(ctx, &backoff)
			continue
		}

		// Mark Up (once).
		m.setUp(ctx, cl)

		// Monitor health until it fails.
		err = m.monitor(ctx, cl)
		if err != nil {
			m.setDown(ctx, err)
			log.Warn("connection lost", "err", err)
			m.closeClient(ctx, cl)
			m.sleepWithBackoff(ctx, &backoff)
			continue
		}

		// rest backoff
		backoff.Duration = 500 * time.Millisecond
	}

}

// ensureClient creates/starts a new client if needed.
// prevents multiple clients from being spawn
func (m *DSConnManager) ensureClient(ctx context.Context) (dsclient.Client, error) {
	// Fast-path: already Up with a client.
	if c := m.Client(); c != nil {
		return c, nil
	}

	// Serialize creation + allow waiting for an in-flight connect.
	m.mu.Lock()
	for {
		// If some client already exists (maybe connecting), reuse it.
		if m.client != nil {
			cl := m.client
			m.mu.Unlock()
			return cl, nil
		}
		// If someone else is dialing, wait.
		if m.connecting {
			m.cond.Wait()
			continue
		}
		// We'll dial.
		m.connecting = true
		break
	}
	m.mu.Unlock()

	// Dial outside the lock.
	cl, err := dsclient.New(m.cfg)
	if err == nil {
		err = cl.Start(ctx) // with Option A, this blocks until Ready
	}

	// Publish result + wake waiters.
	m.mu.Lock()
	if err != nil {
		m.connecting = false
		m.cond.Broadcast()
		m.mu.Unlock()
		if cl != nil {
			cl.Stop(ctx)
		}
		return nil, fmt.Errorf("dataserver connect: %w", err)
	}

	// Store client even before setting DSUp, so subsequent ensureClient() calls reuse it.
	m.client = cl
	m.connecting = false
	m.cond.Broadcast()
	m.mu.Unlock()

	return cl, nil
}

// monitor runs a lightweight health loop.
func (m *DSConnManager) monitor(ctx context.Context, client dsclient.Client) error {
	// force it out of idle
	client.Connect()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		st := client.ConnState()

		if st == connectivity.Ready {
			// wait until it changes away from Ready
			ok := client.WaitForStateChange(ctx, connectivity.Ready)
			if !ok {
				return ctx.Err()
			}
			// loop will re-check new state
			continue
		}

		if st == connectivity.Idle {
			client.Connect()
		}

		// treat these as Down
		if st == connectivity.TransientFailure || st == connectivity.Shutdown {
			return fmt.Errorf("grpc state: %s", st)
		}

		// For Idle/Connecting: wait a bit and re-check
		time.Sleep(500 * time.Millisecond)
	}
}

func (m *DSConnManager) setUp(ctx context.Context, cl dsclient.Client) {
	log := log.FromContext(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()

	changed := m.state != DSUp || m.client == nil
	m.state = DSUp
	m.client = cl

	if changed {
		m.broadcast(Event{State: DSUp, At: time.Now()})
		log.Info("dataserver is UP")
	}
}

func (m *DSConnManager) setDown(ctx context.Context, err error) {
	log := log.FromContext(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()

	changed := m.state != DSDown
	m.state = DSDown
	//m.client = nil

	if changed {
		m.broadcast(Event{State: DSDown, Err: err, At: time.Now()})
		log.Warn("dataserver is DOWN", "err", err)
	}
}

func (m *DSConnManager) broadcast(ev Event) {
	m.subsMu.RLock()
	defer m.subsMu.RUnlock()

	for ch := range m.subs {
		// Non-blocking fan-out: drop if subscriber is slow.
		select {
		case ch <- ev:
		default:
		}
	}
}

func (m *DSConnManager) closeClient(ctx context.Context, cl dsclient.Client) {
	if cl == nil {
		return
	}
	cl.Stop(ctx)
	m.mu.Lock()
	if m.client == cl {
		m.client = nil
	}
	m.mu.Unlock()
}

func (m *DSConnManager) sleepWithBackoff(ctx context.Context, b *wait.Backoff) {
	// Compute next backoff duration (bounded).
	d := Step(b)
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// Step returns a duration using the backoff parameters.
func Step(b *wait.Backoff) time.Duration {
	// This is a minimal re-implementation to avoid pulling more helpers.
	// Duration grows by Factor until Cap.
	d := b.Duration
	if d <= 0 {
		d = 500 * time.Millisecond
	}
	// Increase for next time
	next := time.Duration(float64(d) * b.Factor)
	if b.Cap > 0 && next > b.Cap {
		next = b.Cap
	}
	b.Duration = next
	// Jitter
	if b.Jitter > 0 {
		j := time.Duration(float64(d) * b.Jitter)
		if j > 0 {
			d = d - j/2 + time.Duration(rand.Int64N(int64(j)))
		}
	}
	return d
}

func (m *DSConnManager) closeAllSubs() {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	for ch := range m.subs {
		close(ch)
		delete(m.subs, ch)
	}
}
