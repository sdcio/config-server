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
	"sync"
	"testing"
	"time"

	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/data-server/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestRuntime builds a runtime without DS or k8s client; it is never
// Start()ed, so only the pure decision logic and wiring are exercised.
func newTestRuntime() *TargetRuntime {
	return NewTargetRuntime(testTargetKey(), nil, nil, nil, nil)
}

func gnmiReq(address string, port uint32) *api.DatastoreSpec {
	return &api.DatastoreSpec{
		Target: api.Target{
			Type:    api.TargetType(invv1alpha1.Protocol_GNMI),
			Address: address,
			Port:    port,
		},
	}
}

func withPort(sub *invv1alpha1.Subscription, port uint32) *invv1alpha1.Subscription {
	sub.Spec.Port = port
	return sub
}

func activeSub(crName string) *invv1alpha1.Subscription {
	return newTestSub(crName, invv1alpha1.Encoding_ASCII,
		onChange(crName, invv1alpha1.AdminState_ENABLED, testPath))
}

func TestCollectorAddress(t *testing.T) {
	cases := map[string]struct {
		desired     *api.DatastoreSpec
		subs        []*invv1alpha1.Subscription
		wantAddress string
		wantReason  string // substring; "" means eligible
	}{
		"nil desired": {
			desired:    nil,
			subs:       []*invv1alpha1.Subscription{activeSub("sub1")},
			wantReason: "no target",
		},
		"empty spec": {
			desired:    &api.DatastoreSpec{},
			subs:       []*invv1alpha1.Subscription{activeSub("sub1")},
			wantReason: "not gnmi",
		},
		"netconf target": {
			desired: &api.DatastoreSpec{
				Target: api.Target{Type: "netconf", Address: "10.0.0.1", Port: 830},
			},
			subs:       []*invv1alpha1.Subscription{activeSub("sub1")},
			wantReason: "not gnmi",
		},
		"empty address": {
			desired:    gnmiReq("", 57400),
			subs:       []*invv1alpha1.Subscription{activeSub("sub1")},
			wantReason: "no address",
		},
		"no subscriptions": {
			desired:    gnmiReq("10.0.0.1", 57400),
			wantReason: "no active subscription paths",
		},
		"only disabled subscriptions": {
			desired: gnmiReq("10.0.0.1", 57400),
			subs: []*invv1alpha1.Subscription{
				newTestSub("sub1", invv1alpha1.Encoding_ASCII,
					onChange("sub1", invv1alpha1.AdminState_DISABLED, testPath)),
			},
			wantReason: "no active subscription paths",
		},
		"no port anywhere": {
			desired:    gnmiReq("10.0.0.1", 0),
			subs:       []*invv1alpha1.Subscription{activeSub("sub1")},
			wantReason: "no port",
		},
		"target port only": {
			desired:     gnmiReq("10.0.0.1", 57400),
			subs:        []*invv1alpha1.Subscription{activeSub("sub1")},
			wantAddress: "10.0.0.1:57400",
		},
		"subscription port only": {
			desired:     gnmiReq("10.0.0.1", 0),
			subs:        []*invv1alpha1.Subscription{withPort(activeSub("sub1"), 50051)},
			wantAddress: "10.0.0.1:50051",
		},
		"subscription port overrides target port": {
			desired:     gnmiReq("10.0.0.1", 57400),
			subs:        []*invv1alpha1.Subscription{withPort(activeSub("sub1"), 50051)},
			wantAddress: "10.0.0.1:50051",
		},
		"protocol is case-insensitive": {
			desired: &api.DatastoreSpec{
				Target: api.Target{Type: "GNMI", Address: "10.0.0.1", Port: 57400},
			},
			subs:        []*invv1alpha1.Subscription{activeSub("sub1")},
			wantAddress: "10.0.0.1:57400",
		},
		"ipv6 address": {
			desired:     gnmiReq("2001:db8::1", 57400),
			subs:        []*invv1alpha1.Subscription{activeSub("sub1")},
			wantAddress: "[2001:db8::1]:57400",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rt := newTestRuntime()
			for _, s := range tc.subs {
				require.NoError(t, rt.subscriptions.AddSubscription(s))
			}

			address, reason := rt.collectorAddress(tc.desired)
			if tc.wantReason != "" {
				assert.Contains(t, reason, tc.wantReason)
				assert.Empty(t, address)
				return
			}
			assert.Empty(t, reason)
			assert.Equal(t, tc.wantAddress, address)
		})
	}
}

func TestSubscriptionsPortPrecedence(t *testing.T) {
	t.Run("lowest nsn with a port wins", func(t *testing.T) {
		subs := NewSubscriptions()
		require.NoError(t, subs.AddSubscription(withPort(activeSub("b-sub"), 50052)))
		require.NoError(t, subs.AddSubscription(withPort(activeSub("a-sub"), 50051)))
		assert.Equal(t, uint(50051), subs.Port())
	})

	t.Run("zero port is ignored", func(t *testing.T) {
		subs := NewSubscriptions()
		require.NoError(t, subs.AddSubscription(activeSub("a-sub")))
		require.NoError(t, subs.AddSubscription(withPort(activeSub("b-sub"), 50052)))
		assert.Equal(t, uint(50052), subs.Port())
	})

	t.Run("delete clears port", func(t *testing.T) {
		subs := NewSubscriptions()
		s := withPort(activeSub("a-sub"), 50051)
		require.NoError(t, subs.AddSubscription(s))
		require.NoError(t, subs.DelSubscription(s))
		assert.Equal(t, uint(0), subs.Port())
	})
}

// Subscription changes must only update state and wake the run loop.
func TestRuntimeSubscriptionWakes(t *testing.T) {
	ctx := context.Background()
	rt := newTestRuntime()
	sub := activeSub("sub1")

	require.NoError(t, rt.UpsertSubscription(ctx, sub))
	assert.Equal(t, 1, len(rt.wakeCh), "upsert must wake")
	assert.True(t, rt.subscriptions.HasActivePaths())
	assert.Nil(t, rt.collector.Load(), "upsert must not touch the collector")

	// coalesces
	require.NoError(t, rt.UpsertSubscription(ctx, sub))
	assert.Equal(t, 1, len(rt.wakeCh))

	<-rt.wakeCh // drain

	require.NoError(t, rt.DeleteSubscription(ctx, sub))
	assert.Equal(t, 1, len(rt.wakeCh), "delete must wake")
	assert.False(t, rt.subscriptions.HasSubscriptions())
	assert.Nil(t, rt.collector.Load(), "delete must not touch the collector")
}

// When the target is no longer eligible, an existing collector is stopped
// and unpublished; when there is none, nothing is created.
func TestReconcileCollectorStopsWhenIneligible(t *testing.T) {
	ctx := context.Background()

	cases := map[string]struct {
		desired *api.DatastoreSpec
		subs    []*invv1alpha1.Subscription
	}{
		"no target": {
			desired: nil,
			subs:    []*invv1alpha1.Subscription{activeSub("sub1")},
		},
		"switched to netconf": {
			desired: &api.DatastoreSpec{
				Target: api.Target{Type: "netconf", Address: "10.0.0.1", Port: 830},
			},
			subs: []*invv1alpha1.Subscription{activeSub("sub1")},
		},
		"last subscription removed": {
			desired: gnmiReq("10.0.0.1", 57400),
		},
		"no port": {
			desired: gnmiReq("10.0.0.1", 0),
			subs:    []*invv1alpha1.Subscription{activeSub("sub1")},
		},
	}

	for name, tc := range cases {
		t.Run(name+"/existing collector is stopped", func(t *testing.T) {
			rt := newTestRuntime()
			for _, s := range tc.subs {
				require.NoError(t, rt.subscriptions.AddSubscription(s))
			}
			rt.collector.Store(NewCollector(rt.key, rt.subscriptions))

			rt.reconcileCollector(ctx, tc.desired)
			assert.Nil(t, rt.collector.Load())
			assert.Nil(t, rt.Cache(), "exporter must not see a stopped collector")
		})

		t.Run(name+"/no collector is created", func(t *testing.T) {
			rt := newTestRuntime()
			for _, s := range tc.subs {
				require.NoError(t, rt.subscriptions.AddSubscription(s))
			}

			rt.reconcileCollector(ctx, tc.desired)
			assert.Nil(t, rt.collector.Load())
		})
	}
}

// A non-recoverable recovery failure latches until the target definition
// (desired hash) or the human retry token changes.
func TestRecoveryLatch(t *testing.T) {
	// latch takes any error; the TransactionError classification happens in
	// reconcileOnce, so a plain error keeps this test independent of its fields
	failure := errors.New(`intent "cfg1" error: "invalid value"`)

	t.Run("latched for same hash and token", func(t *testing.T) {
		rt := newTestRuntime()
		rt.SetDesired(gnmiReq("10.0.0.1", 57400), nil, "h1")
		rt.SetRecoveryRetryToken("t1")
		rt.latchRecoveryFailure(failure)

		err, latched := rt.recoveryLatched()
		assert.True(t, latched)
		assert.Equal(t, failure, err)
	})

	t.Run("survives datastore reset", func(t *testing.T) {
		rt := newTestRuntime()
		rt.SetDesired(gnmiReq("10.0.0.1", 57400), nil, "h1")
		rt.latchRecoveryFailure(failure)

		rt.setRecovered(false)
		rt.setDatastoreReady(false)
		_, latched := rt.recoveryLatched()
		assert.True(t, latched)
	})

	t.Run("released by new retry token", func(t *testing.T) {
		rt := newTestRuntime()
		rt.SetDesired(gnmiReq("10.0.0.1", 57400), nil, "h1")
		rt.SetRecoveryRetryToken("t1")
		rt.latchRecoveryFailure(failure)
		<-rt.wakeCh // drain

		rt.SetRecoveryRetryToken("t2")
		_, latched := rt.recoveryLatched()
		assert.False(t, latched)
		assert.Equal(t, 1, len(rt.wakeCh), "new token must wake the runtime")
	})

	t.Run("same token does not wake", func(t *testing.T) {
		rt := newTestRuntime()
		rt.SetRecoveryRetryToken("t1")
		<-rt.wakeCh // drain
		rt.SetRecoveryRetryToken("t1")
		assert.Equal(t, 0, len(rt.wakeCh))
	})

	t.Run("released by target definition change", func(t *testing.T) {
		rt := newTestRuntime()
		rt.SetDesired(gnmiReq("10.0.0.1", 57400), nil, "h1")
		rt.latchRecoveryFailure(failure)

		rt.SetDesired(gnmiReq("10.0.0.2", 57400), nil, "h2")
		_, latched := rt.recoveryLatched()
		assert.False(t, latched)
	})

	t.Run("cleared by successful recovery", func(t *testing.T) {
		rt := newTestRuntime()
		rt.SetDesired(gnmiReq("10.0.0.1", 57400), nil, "h1")
		rt.latchRecoveryFailure(failure)

		rt.setRecoveryResult("", nil)
		_, latched := rt.recoveryLatched()
		assert.False(t, latched)
	})

	t.Run("status carries the failure for the Ready condition", func(t *testing.T) {
		rt := newTestRuntime()
		rt.SetDesired(gnmiReq("10.0.0.1", 57400), nil, "h1")
		rt.latchRecoveryFailure(failure)
		rt.setPhase(context.Background(), PhaseRecoveryFailed, failure)

		st := rt.Status()
		assert.Equal(t, PhaseRecoveryFailed, st.Phase)
		assert.False(t, st.Recovered)
		assert.Equal(t, failure.Error(), st.LastError)
	})
}

// The runtime notifies the Ready-condition owner only when its status changes.
func TestRuntimeNotifiesOnStatusChange(t *testing.T) {
	ctx := context.Background()
	calls := 0
	rt := NewTargetRuntime(testTargetKey(), nil, nil, nil, func() { calls++ })

	rt.notifyIfChanged()
	assert.Equal(t, 1, calls, "first status is always notified")

	rt.notifyIfChanged()
	assert.Equal(t, 1, calls, "unchanged status must not notify")

	rt.setPhase(ctx, PhaseRecovering, nil)
	rt.notifyIfChanged()
	assert.Equal(t, 2, calls, "phase change must notify")

	rt.setPhase(ctx, PhaseRecovering, errors.New("not connected"))
	rt.notifyIfChanged()
	assert.Equal(t, 3, calls, "error change must notify")

	rt.setRecoveryResult("1 warning", nil)
	rt.notifyIfChanged()
	assert.Equal(t, 4, calls, "recovery result must notify")
	assert.Equal(t, "1 warning", rt.Status().RecoveryMsg)
}

// Stop on a runtime that was never started must not block.
func TestRuntimeStopNotStarted(t *testing.T) {
	rt := newTestRuntime()
	assert.NoError(t, rt.Stop(time.Second))
}

// Stop succeeds only once the loop has exited; a retry after a timeout keeps
// waiting for the same loop (sdcio/config-server#516).
func TestRuntimeStopWaitsForRunLoop(t *testing.T) {
	rt := newTestRuntime()
	rt.startOnce.Do(func() {}) // pretend the loop is running

	assert.Error(t, rt.Stop(20*time.Millisecond), "loop has not exited yet")
	select {
	case <-rt.stopCh:
	default:
		t.Fatal("Stop must signal the loop")
	}
	assert.Error(t, rt.Stop(20*time.Millisecond), "retry keeps waiting, no double close")

	close(rt.done) // loop exits
	assert.NoError(t, rt.Stop(time.Second))
}

// Concurrent Start/Stop never panics (double close) and Stop always returns.
func TestRuntimeConcurrentStartStop(t *testing.T) {
	for i := 0; i < 100; i++ {
		rt := newTestRuntime()
		rt.startOnce.Do(func() { close(rt.done) }) // a loop that exits at once

		var wg sync.WaitGroup
		for j := 0; j < 4; j++ {
			wg.Add(2)
			go func() { defer wg.Done(); rt.Start(context.Background()) }()
			go func() { defer wg.Done(); _ = rt.Stop(time.Second) }()
		}
		wg.Wait()
		assert.NoError(t, rt.Stop(time.Second))
	}
}
