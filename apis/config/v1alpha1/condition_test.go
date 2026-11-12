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

package v1alpha1

import (
	"strings"
	"testing"

	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestConfigFailedUnrecoverable(t *testing.T) {
	const msg = "apply failed: malformed intent"

	cond := ConfigFailedUnrecoverable(7, msg)

	if cond.Type != string(ConditionTypeConfigReady) {
		t.Errorf("Type = %q, want %q", cond.Type, ConditionTypeConfigReady)
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("Status = %v, want %v", cond.Status, metav1.ConditionFalse)
	}
	if cond.Reason != string(condv1alpha1.ConditionReasonUnrecoverable) {
		t.Errorf("Reason = %q, want %q (Config.IsRecoverable keys off this Reason)", cond.Reason, condv1alpha1.ConditionReasonUnrecoverable)
	}
	if cond.ObservedGeneration != 7 {
		t.Errorf("ObservedGeneration = %d, want 7 (Config.IsRecoverable keys off it)", cond.ObservedGeneration)
	}
	if cond.Message != msg {
		t.Errorf("Message = %q, want %q: a plain message, no JSON payload", cond.Message, msg)
	}
}

// A long dataserver error must stay bounded: the message is plain text now,
// so truncating it cannot break IsRecoverable.
func TestConfigFailedUnrecoverableBoundedMessage(t *testing.T) {
	long := strings.Repeat("x", 3*condv1alpha1.MaxMessageBytes)
	cond := ConfigFailedUnrecoverable(1, long)
	if len(cond.Message) > condv1alpha1.MaxMessageBytes {
		t.Errorf("len(Message) = %d, want <= %d", len(cond.Message), condv1alpha1.MaxMessageBytes)
	}
}

func TestReadyFromApplied(t *testing.T) {
	tests := map[string]struct {
		applied    *condv1alpha1.Condition
		wantStatus metav1.ConditionStatus
		wantReason condv1alpha1.ConditionReason
		wantMsg    string
	}{
		"never transacted": {
			wantStatus: metav1.ConditionFalse,
			wantReason: ConfigReasonPending,
			wantMsg:    "config not applied yet",
		},
		"applied": {
			applied:    ptrTo(ConfigReady(1, "")),
			wantStatus: metav1.ConditionTrue,
			wantReason: condv1alpha1.ConditionReasonReady,
		},
		"failed": {
			applied:    ptrTo(ConfigFailed(1, "transient EOF")),
			wantStatus: metav1.ConditionFalse,
			wantReason: condv1alpha1.ConditionReasonFailed,
			wantMsg:    "transient EOF",
		},
		"unrecoverable": {
			applied:    ptrTo(ConfigFailedUnrecoverable(1, "malformed intent")),
			wantStatus: metav1.ConditionFalse,
			wantReason: condv1alpha1.ConditionReasonUnrecoverable,
			wantMsg:    "malformed intent",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := &Config{}
			if tc.applied != nil {
				cfg.SetConditions(*tc.applied)
			}
			got := cfg.ReadyFromApplied()
			if got.Type != string(condv1alpha1.ConditionTypeReady) {
				t.Errorf("Type = %q, want Ready", got.Type)
			}
			if got.Status != tc.wantStatus || got.Reason != string(tc.wantReason) || got.Message != tc.wantMsg {
				t.Errorf("got %s/%s %q, want %s/%s %q",
					got.Status, got.Reason, got.Message, tc.wantStatus, tc.wantReason, tc.wantMsg)
			}
			// Ready carries the generation of the outcome it follows
			if tc.applied != nil && got.ObservedGeneration != tc.applied.ObservedGeneration {
				t.Errorf("ObservedGeneration = %d, want %d", got.ObservedGeneration, tc.applied.ObservedGeneration)
			}
		})
	}
}

// sdcio/config-server#469 / PR #520 review: with the target missing, Ready
// names the target, and ConfigReady (the transaction state) is untouched.
func TestConfigNotReadyKeepsTransactionState(t *testing.T) {
	cfg := &Config{}
	cfg.SetConditions(ConfigFailedUnrecoverable(3, "malformed intent"))
	cfg.SetConditions(ConfigNotReady(3, ConfigReasonTargetNotFound, "target ns/t1 not found"))

	ready := cfg.GetCondition(condv1alpha1.ConditionTypeReady)
	if ready.Status != metav1.ConditionFalse || ready.Reason != string(ConfigReasonTargetNotFound) {
		t.Errorf("Ready = %s/%s, want False/%s", ready.Status, ready.Reason, ConfigReasonTargetNotFound)
	}
	applied := cfg.GetCondition(ConditionTypeConfigReady)
	if applied.Reason != string(condv1alpha1.ConditionReasonUnrecoverable) || applied.ObservedGeneration != 3 {
		t.Errorf("ConfigReady = %s gen %d, want Unrecoverable gen 3", applied.Reason, applied.ObservedGeneration)
	}
	if !cfg.Status.HasCondition(ConditionTypeConfigReady) || len(cfg.Status.Conditions) != 2 {
		t.Errorf("want exactly Ready and ConfigReady, got %v", cfg.Status.Conditions)
	}
}

func TestIsRecoverable(t *testing.T) {
	cfg := &Config{ObjectMeta: metav1.ObjectMeta{Generation: 3}}
	if !cfg.IsRecoverable() {
		t.Error("no ConfigReady: want recoverable")
	}

	cfg.SetConditions(ConfigFailed(3, "transient EOF"))
	if !cfg.IsRecoverable() {
		t.Error("Failed: want recoverable")
	}

	cfg.SetConditions(ConfigFailedUnrecoverable(3, "malformed intent"))
	if cfg.IsRecoverable() {
		t.Error("Unrecoverable for the current generation: want not recoverable")
	}

	cfg.Generation = 4 // spec changed
	if !cfg.IsRecoverable() {
		t.Error("Unrecoverable for an older generation: want recoverable")
	}
}

// Equal must see a generation change, else a new Unrecoverable with the same
// message is skipped as no-op and IsRecoverable keeps retrying it.
func TestConditionEqualObservedGeneration(t *testing.T) {
	if ConfigFailedUnrecoverable(1, "x").Equal(ConfigFailedUnrecoverable(2, "x")) {
		t.Error("conditions with a different ObservedGeneration must not be equal")
	}
}

func ptrTo[T any](v T) *T { return &v }
