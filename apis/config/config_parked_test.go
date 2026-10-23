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

func parkedConfig(ctx context.Context, resourceVersion string) *Config {
	cfg := &Config{}
	cfg.Name = "intent1"
	cfg.ResourceVersion = resourceVersion
	cfg.Spec.Priority = 10
	cfg.Status.Parked = &ConfigParked{SpecHash: cfg.SpecHash(ctx), Attempts: 1}
	cfg.SetConditions(unrecoverableCond("validation failed"))
	return cfg
}

func TestIsRecoverable_ParkedSurvivesOwnStatusWrite(t *testing.T) {
	ctx := context.Background()
	cfg := parkedConfig(ctx, "10")
	if cfg.IsRecoverable(ctx) {
		t.Fatal("parked config must not be recoverable")
	}

	// every status write bumps resourceVersion; the parked record must stay valid
	cfg.ResourceVersion = "11"
	if cfg.IsRecoverable(ctx) {
		t.Fatal("parked record must survive a resourceVersion bump with the same spec")
	}
}

func TestIsRecoverable_SpecChangeInvalidatesParked(t *testing.T) {
	ctx := context.Background()
	cfg := parkedConfig(ctx, "10")

	cfg.Spec.Priority = 20
	if !cfg.IsRecoverable(ctx) {
		t.Fatal("spec change must make a parked config recoverable again")
	}
}

func TestIsRecoverable_UnrecoverableWithoutParkedIsRecoverable(t *testing.T) {
	ctx := context.Background()
	cfg := &Config{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "10"}}
	cfg.SetConditions(unrecoverableCond("legacy json marker"))

	if !cfg.IsRecoverable(ctx) {
		t.Fatal("an Unrecoverable condition without status.parked must not park the config")
	}
}

func TestIsRecoverable_ParkedRecordIgnoredOnceReasonIsNotUnrecoverable(t *testing.T) {
	ctx := context.Background()
	cfg := parkedConfig(ctx, "10")
	cfg.SetConditions(condition.Condition{Condition: metav1.Condition{
		Type:    string(ConditionTypeConfigReady),
		Status:  metav1.ConditionFalse,
		Reason:  string(condition.ConditionReasonFailed),
		Message: "retry requested",
	}})

	if !cfg.IsRecoverable(ctx) {
		t.Fatal("a woken config keeps status.parked but must be retried")
	}
}
