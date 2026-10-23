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

package config

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sdcio/config-server/apis/condition"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func unrecoverableCond(msg string) condition.Condition {
	return condition.Condition{Condition: metav1.Condition{
		Type:    string(ConditionTypeConfigReady),
		Status:  metav1.ConditionFalse,
		Reason:  string(condition.ConditionReasonUnrecoverable),
		Message: msg,
	}}
}

func parkedConfig(t *testing.T, ctx context.Context, resourceVersion string) *Config {
	t.Helper()
	cfg := &Config{}
	cfg.Name = "intent1"
	cfg.ResourceVersion = resourceVersion
	cfg.Spec.Priority = 10

	marker, err := json.Marshal(condition.UnrecoverableMessage{
		SpecHash: cfg.SpecHash(ctx),
		Message:  "validation failed",
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg.SetConditions(unrecoverableCond(string(marker)))
	return cfg
}

func TestIsRecoverable_ParkedMarkerSurvivesOwnStatusWrite(t *testing.T) {
	ctx := context.Background()
	cfg := parkedConfig(t, ctx, "10")
	if cfg.IsRecoverable(ctx) {
		t.Fatal("parked config must not be recoverable")
	}

	// every status write bumps resourceVersion; the marker must stay valid
	cfg.ResourceVersion = "11"
	if cfg.IsRecoverable(ctx) {
		t.Fatal("marker must survive a resourceVersion bump with the same spec")
	}
}

func TestIsRecoverable_SpecChangeInvalidatesMarker(t *testing.T) {
	ctx := context.Background()
	cfg := parkedConfig(t, ctx, "10")

	cfg.Spec.Priority = 20
	if !cfg.IsRecoverable(ctx) {
		t.Fatal("spec change must make a parked config recoverable again")
	}
}

func TestIsRecoverable_MarkerWithoutSpecHashIsRecoverable(t *testing.T) {
	ctx := context.Background()
	cfg := &Config{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "10"}}
	legacy, _ := json.Marshal(condition.UnrecoverableMessage{ResourceVersion: "10", Message: "old"})
	cfg.SetConditions(unrecoverableCond(string(legacy)))

	if !cfg.IsRecoverable(ctx) {
		t.Fatal("legacy resourceVersion-only marker must not park the config")
	}
}
