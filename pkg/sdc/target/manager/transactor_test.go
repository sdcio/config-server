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
	"encoding/json"
	"testing"

	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	internalconfig "github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// These tests exercise the exact condition-computation chain that
// Transactor.updateConfigWithError's non-recoverable branch relies on
// (configv1alpha1.ConfigFailedUnrecoverable -> GetOverallCondition ->
// DedupeConditions), reproducing it inline rather than going through
// updateConfigWithError's client.Status().Apply call, since Server-Side
// Apply against a CRD status subresource isn't reliably emulated by the
// controller-runtime fake client. This is the exact logic that was buggy:
// before the fix, condv1alpha1.FailedUnRecoverable stamped Type: "Ready"
// (colliding with the overall condition's Type), so DedupeConditions
// silently dropped the failure and the persisted status kept a stale
// Ready=True.
func newConfigWithPriorSuccess(name string) *configv1alpha1.Config {
	cfg := &configv1alpha1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       "default",
			ResourceVersion: "10",
		},
	}
	cfg.SetConditions(
		configv1alpha1.ConfigReady("applied"),
		configv1alpha1.TargetForConfigReady("target ready"),
	)
	return cfg
}

func TestUpdateConfigWithError_NonRecoverable_StampsConfigReadyAndOverallFalse(t *testing.T) {
	current := newConfigWithPriorSuccess("cfg-nonrecoverable")

	newMessage := condv1alpha1.UnrecoverableMessage{
		ResourceVersion: current.GetResourceVersion(),
		Message:         "apply failed: malformed intent",
	}
	raw, err := json.Marshal(newMessage)
	require.NoError(t, err)

	newConfigCond := configv1alpha1.ConfigFailedUnrecoverable(string(raw))

	// Same Type as ConfigFailed/ConfigReady: this is what makes SetConditions
	// below overwrite the real ConfigReady slot instead of colliding with the
	// generic top-level Ready slot.
	require.Equal(t, string(configv1alpha1.ConditionTypeConfigReady), newConfigCond.Type)

	tmp := current.DeepCopy()
	tmp.SetConditions(newConfigCond)
	newOverallCond := configv1alpha1.GetOverallCondition(tmp)

	assert.Equal(t, metav1.ConditionFalse, newOverallCond.Status,
		"overall Ready must be recomputed to False, not remain stale True from before the failure")

	deduped := condv1alpha1.DedupeConditions(newConfigCond, newOverallCond)
	require.Len(t, deduped, 2,
		"ConfigReady and Ready are different Types now, so both must survive dedupe (pre-fix, both were Type Ready and only one survived)")

	byType := map[string]condv1alpha1.Condition{}
	for _, c := range deduped {
		byType[c.Type] = c
	}

	cfgReady, ok := byType[string(configv1alpha1.ConditionTypeConfigReady)]
	require.True(t, ok, "deduped conditions must include ConfigReady")
	assert.Equal(t, metav1.ConditionFalse, cfgReady.Status)
	assert.Equal(t, string(condv1alpha1.ConditionReasonUnrecoverable), cfgReady.Reason)

	overallReady, ok := byType[string(condv1alpha1.ConditionTypeReady)]
	require.True(t, ok, "deduped conditions must include the overall Ready")
	assert.Equal(t, metav1.ConditionFalse, overallReady.Status)

	// Apply the resulting condition onto the actual object (as updateConfigWithError
	// does before persisting) and confirm the internal Config.IsRecoverable(ctx)
	// contract this bug fix is supposed to restore.
	final := current.DeepCopy()
	final.SetConditions(deduped...)

	internalCfg := toInternalConfig(t, final)
	ctx := context.Background()
	assert.False(t, internalCfg.IsRecoverable(ctx), "IsRecoverable must report false immediately after (same ResourceVersion)")

	// Simulate a spec edit bumping ResourceVersion: IsRecoverable should flip back to true.
	internalCfg.ResourceVersion = "11"
	assert.True(t, internalCfg.IsRecoverable(ctx), "IsRecoverable must flip back to true once ResourceVersion changes (spec edit)")
}

func TestUpdateConfigWithError_Recoverable_UnchangedBehavior(t *testing.T) {
	current := newConfigWithPriorSuccess("cfg-recoverable")

	newConfigCond := configv1alpha1.ConfigFailed("apply failed: transient EOF")
	require.Equal(t, string(configv1alpha1.ConditionTypeConfigReady), newConfigCond.Type)
	require.Equal(t, string(condv1alpha1.ConditionReasonFailed), newConfigCond.Reason)

	tmp := current.DeepCopy()
	tmp.SetConditions(newConfigCond)
	newOverallCond := configv1alpha1.GetOverallCondition(tmp)
	assert.Equal(t, metav1.ConditionFalse, newOverallCond.Status)

	deduped := condv1alpha1.DedupeConditions(newConfigCond, newOverallCond)
	require.Len(t, deduped, 2)

	final := current.DeepCopy()
	final.SetConditions(deduped...)

	internalCfg := toInternalConfig(t, final)
	assert.True(t, internalCfg.IsRecoverable(context.Background()),
		"recoverable failures must still report IsRecoverable == true (regression guard for the untouched branch)")
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
