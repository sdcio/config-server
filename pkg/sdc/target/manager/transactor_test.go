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

	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	internalconfig "github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// These tests exercise the condition chain updateConfigWithError builds
// (ConfigFailed[Unrecoverable] -> ReadyFromApplied) and the internal
// Config.IsRecoverable(ctx) the transaction classification relies on,
// without the client.Status().Apply call (SSA on a status subresource is not
// reliably emulated by the controller-runtime fake client).
func newConfigWithPriorSuccess(name string) *configv1alpha1.Config {
	cfg := &configv1alpha1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  "default",
			Generation: 10,
		},
	}
	cfg.SetConditions(configv1alpha1.ConfigReady(10, ""))
	cfg.SetConditions(cfg.ReadyFromApplied())
	return cfg
}

func TestUpdateConfigWithError_NonRecoverable(t *testing.T) {
	cfg := newConfigWithPriorSuccess("cfg-nonrecoverable")

	cfg.SetConditions(configv1alpha1.ConfigFailedUnrecoverable(cfg.GetGeneration(), "apply failed: malformed intent"))
	cfg.SetConditions(cfg.ReadyFromApplied())

	applied := cfg.GetCondition(configv1alpha1.ConditionTypeConfigReady)
	assert.Equal(t, metav1.ConditionFalse, applied.Status)
	assert.Equal(t, string(condv1alpha1.ConditionReasonUnrecoverable), applied.Reason)
	assert.Equal(t, int64(10), applied.ObservedGeneration)

	ready := cfg.GetCondition(condv1alpha1.ConditionTypeReady)
	assert.Equal(t, metav1.ConditionFalse, ready.Status, "Ready must not stay True after the failure")
	assert.Equal(t, string(condv1alpha1.ConditionReasonUnrecoverable), ready.Reason)
	assert.Equal(t, "apply failed: malformed intent", ready.Message)

	internalCfg := toInternalConfig(t, cfg)
	ctx := context.Background()
	assert.False(t, internalCfg.IsRecoverable(ctx), "same generation: not recoverable")

	// a spec edit bumps the generation: recoverable again
	internalCfg.Generation = 11
	assert.True(t, internalCfg.IsRecoverable(ctx), "new generation: recoverable")
}

func TestUpdateConfigWithError_Recoverable(t *testing.T) {
	cfg := newConfigWithPriorSuccess("cfg-recoverable")

	cfg.SetConditions(configv1alpha1.ConfigFailed(cfg.GetGeneration(), "apply failed: transient EOF"))
	cfg.SetConditions(cfg.ReadyFromApplied())

	ready := cfg.GetCondition(condv1alpha1.ConditionTypeReady)
	assert.Equal(t, metav1.ConditionFalse, ready.Status)
	assert.Equal(t, string(condv1alpha1.ConditionReasonFailed), ready.Reason)

	internalCfg := toInternalConfig(t, cfg)
	assert.True(t, internalCfg.IsRecoverable(context.Background()))
	assert.False(t, internalCfg.IsConfigConditionReady(), "a failed config is retried")
}

// A target outage only changes Ready: the transaction state survives, so an
// unrecoverable config stays out of the next transaction.
func TestTargetNotReady_KeepsUnrecoverable(t *testing.T) {
	cfg := newConfigWithPriorSuccess("cfg-target-down")
	cfg.SetConditions(configv1alpha1.ConfigFailedUnrecoverable(cfg.GetGeneration(), "malformed intent"))
	cfg.SetConditions(configv1alpha1.ConfigNotReady(configv1alpha1.ConfigReasonTargetNotReady, "Recovering: recovering configs"))

	assert.Equal(t, string(configv1alpha1.ConfigReasonTargetNotReady),
		cfg.GetCondition(condv1alpha1.ConditionTypeReady).Reason)
	assert.False(t, toInternalConfig(t, cfg).IsRecoverable(context.Background()))
}

// toInternalConfig converts a versioned configv1alpha1.Config to the internal
// config.Config type, mirroring the conversion the apiserver performs before
// getConfigsToTransact operates on it, so tests exercise the exact
// Config.IsRecoverable(ctx) implementation the transactor relies on.
func toInternalConfig(t *testing.T, in *configv1alpha1.Config) *internalconfig.Config {
	t.Helper()
	out := &internalconfig.Config{}
	require.NoError(t, configv1alpha1.Convert_v1alpha1_Config_To_config_Config(in, out, nil))
	return out
}

// The dataserver collects validation errors concurrently, so the same failure
// comes back with its errors in a different order. The message built from them
// must not change, or every retry rewrites the Config status.
func TestIntentErrorsStableOrder(t *testing.T) {
	rsp := func(errs ...string) *sdcpb.TransactionSetResponse {
		return &sdcpb.TransactionSetResponse{Intents: map[string]*sdcpb.TransactionSetResponseIntent{
			"cfg-b": {Errors: errs},
			"cfg-a": {Errors: []string{"leafref target missing"}},
		}}
	}
	first := rsp("mandatory leaf missing", "value out of range")
	second := rsp("value out of range", "mandatory leaf missing")

	assert.Equal(t, []string{"cfg-a", "cfg-b"}, sortedIntentNames(first))
	assert.Equal(t,
		sortedIntentErrors(first.Intents["cfg-b"]),
		sortedIntentErrors(second.Intents["cfg-b"]))
	assert.Equal(t, []string{"value out of range", "mandatory leaf missing"},
		second.Intents["cfg-b"].Errors, "the response itself is left untouched")

	r1, r2 := analyzeIntentResponse(nil, first), analyzeIntentResponse(nil, second)
	require.Error(t, r1.IntentErrors)
	assert.Equal(t, r1.IntentErrors.Error(), r2.IntentErrors.Error())
	assert.False(t, r1.Recoverable)
}

func TestSortedIntentHelpersNil(t *testing.T) {
	assert.Nil(t, sortedIntentNames(nil))
	assert.Nil(t, sortedIntentErrors(nil))
}
