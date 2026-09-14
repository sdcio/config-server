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

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// newTestRuntime builds a TargetRuntime backed by a fake k8s client seeded
// with a Target whose TargetConnectionReady condition already matches what
// reconcileOnce's deferred pushConnIfChanged would compute for a
// not-yet-ds-ready target. This lets tests call reconcileOnce directly
// without needing the fake client to support status-subresource Apply.
func newTestRuntime(t *testing.T) *TargetRuntime {
	t.Helper()
	sch := runtime.NewScheme()
	if err := configv1alpha1.AddToScheme(sch); err != nil {
		t.Fatalf("add configv1alpha1 to scheme: %v", err)
	}
	nsn := types.NamespacedName{Namespace: "default", Name: "target1"}
	target := &configv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{Name: nsn.Name, Namespace: nsn.Namespace},
	}
	target.Status.SetConditions(configv1alpha1.TargetConnectionFailed("dataserver not ready"))
	fakeClient := fake.NewClientBuilder().WithScheme(sch).WithObjects(target).WithStatusSubresource(target).Build()

	return &TargetRuntime{
		key:    storebackend.KeyFromNSN(nsn),
		client: fakeClient,
		phase:  PhaseRunning,
		wakeCh: make(chan struct{}, 1),
	}
}

// processWake mirrors run()'s own wakeCh handling: a queued signal triggers
// exactly one reconcileOnce call, and an empty channel is a pure no-op. Using
// this (instead of calling reconcileOnce unconditionally) is what makes the
// Phase assertions below a genuine regression test for the no-op-hash guard:
// against the old unconditional-wake code, the unchanged-hash case would
// have queued a signal here too, driving reconcileOnce and perturbing Phase.
func processWake(ctx context.Context, rt *TargetRuntime) {
	select {
	case <-rt.wakeCh:
		rt.reconcileOnce(ctx)
	default:
	}
}

func TestSetDesired_SkipsWakeOnUnchangedHash(t *testing.T) {
	ctx := context.Background()
	rt := newTestRuntime(t)
	req := &sdcpb.CreateDataStoreRequest{}

	// First call establishes the desired hash; since it differs from the
	// zero-value initial desiredHash, it must still wake and reconcile,
	// settling on PhaseWaitingForDS since no dataserver is wired up here.
	rt.SetDesired(req, nil, "hash-a")
	processWake(ctx, rt)
	assert.Equal(t, PhaseWaitingForDS, rt.Status().Phase)

	// Simulate the runtime having since converged to PhaseRunning.
	rt.setPhase(ctx, PhaseRunning, nil)

	// Second call with an identical hash must not wake: nothing about the
	// desired state changed, so no reconcile should be triggered and Phase
	// must never be perturbed away from PhaseRunning.
	rt.SetDesired(req, nil, "hash-a")
	processWake(ctx, rt)
	assert.Equal(t, PhaseRunning, rt.Status().Phase, "Phase must not transition off PhaseRunning for a no-op SetDesired call")
}

func TestSetDesired_WakesOnChangedHash(t *testing.T) {
	ctx := context.Background()
	rt := newTestRuntime(t)
	req := &sdcpb.CreateDataStoreRequest{}

	rt.SetDesired(req, nil, "hash-a")
	processWake(ctx, rt)
	rt.setPhase(ctx, PhaseRunning, nil) // simulate having since converged

	// A genuinely different hash must still wake, and the resulting
	// reconcile must still cycle the phase off PhaseRunning (the legitimate
	// re-apply/recreate path is unaffected by the no-op guard).
	rt.SetDesired(req, nil, "hash-b")
	processWake(ctx, rt)
	assert.NotEqual(t, PhaseRunning, rt.Status().Phase, "reconcileOnce should have cycled Phase off PhaseRunning for a real desired-state change")
}

func TestAnalyzeIntentResponse(t *testing.T) {
	cases := map[string]struct {
		rsp              *sdcpb.TransactionSetResponse
		err              error
		expectErrors     bool
		recoverable      bool
		globalErrContain string
		intentErrContain string
		warnContain      string
	}{
		"Success_NoWarningsOrErrors": {
			rsp:          &sdcpb.TransactionSetResponse{},
			err:          nil,
			expectErrors: false,
		},
		"Recoverable gRPC Error": {
			rsp:              nil,
			err:              status.Error(codes.ResourceExhausted, "quota exceeded"),
			expectErrors:     true,
			recoverable:      true,
			globalErrContain: "quota exceeded",
		},
		"Non-Recoverable gRPC Error": {
			rsp:              nil,
			err:              status.Error(codes.InvalidArgument, "invalid request"),
			expectErrors:     true,
			recoverable:      false,
			globalErrContain: "invalid request",
		},
		"Intent Errors in Response": {
			rsp: &sdcpb.TransactionSetResponse{
				Intents: map[string]*sdcpb.TransactionSetResponseIntent{
					"intent1": {Errors: []string{"failed to apply update"}},
				},
			},
			err:              nil,
			expectErrors:     true,
			recoverable:      false,
			intentErrContain: "failed to apply update",
		},
		"Global Warnings Only": {
			rsp: &sdcpb.TransactionSetResponse{
				Warnings: []string{"slow transaction detected"},
			},
			err:          nil,
			expectErrors: false,
			warnContain:  "slow transaction detected",
		},
		"Intent Warnings Only": {
			rsp: &sdcpb.TransactionSetResponse{
				Intents: map[string]*sdcpb.TransactionSetResponseIntent{
					"intent1": {Warnings: []string{"potential inconsistency"}},
				},
			},
			err:          nil,
			expectErrors: false,
			warnContain:  "potential inconsistency",
		},
		"Both Errors and Warnings": {
			rsp: &sdcpb.TransactionSetResponse{
				Warnings: []string{"global slow transaction"},
				Intents: map[string]*sdcpb.TransactionSetResponseIntent{
					"intent1": {
						Warnings: []string{"intent warning"},
						Errors:   []string{"intent error"},
					},
				},
			},
			err:              nil,
			expectErrors:     true,
			recoverable:      false,
			intentErrContain: "intent error",
			warnContain:      "global slow transaction",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			result := AnalyzeIntentResponse(tc.err, tc.rsp)

			if tc.expectErrors {
				assert.True(t, result.HasErrors(), "expected HasErrors() == true")
				assert.Equal(t, tc.recoverable, result.Recoverable,
					"Recoverable mismatch")

				if tc.globalErrContain != "" {
					assert.NotNil(t, result.GlobalError)
					assert.Contains(t, result.GlobalError.Error(), tc.globalErrContain)
				}
				if tc.intentErrContain != "" {
					assert.NotNil(t, result.IntentErrors)
					assert.Contains(t, result.IntentErrors.Error(), tc.intentErrContain)
				}
			} else {
				assert.False(t, result.HasErrors(), "expected HasErrors() == false")
				assert.Nil(t, result.GlobalError)
				assert.Nil(t, result.IntentErrors)
			}

			if tc.warnContain != "" {
				found := false
				for _, w := range result.GlobalWarnings {
					if contains(w, tc.warnContain) {
						found = true
						break
					}
				}
				assert.True(t, found,
					"expected warning containing %q in %v", tc.warnContain, result.GlobalWarnings)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}