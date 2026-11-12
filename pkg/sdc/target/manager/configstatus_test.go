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
	"testing"

	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var readyTestTarget = &configv1alpha1.Target{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "dev1"}}

func newUnrecoverableConfig(name string, generation int64) *configv1alpha1.Config {
	cfg := newTargetConfig(name, 10)
	cfg.Generation = generation
	cfg.SetConditions(configv1alpha1.ConfigFailedUnrecoverable(generation, "malformed intent"))
	return cfg
}

func listTargetConfigs(t *testing.T, c client.Client) *configv1alpha1.ConfigList {
	t.Helper()
	cfgs, err := ListTargetConfigs(context.Background(), c, readyTestTarget)
	require.NoError(t, err)
	return cfgs
}

// Without outcomes, Ready follows the listed ConfigReady and carries its
// observedGeneration.
func TestUpdateConfigsReady_FollowsListedConfigReady(t *testing.T) {
	c := newFakeConfigClient(t, nil, newUnrecoverableConfig("base", 3))
	tr := NewTransactor(c)

	require.NoError(t, tr.UpdateConfigsReady(context.Background(), listTargetConfigs(t, c), readyFromOutcomes(nil)))

	ready := getTargetConfig(t, c, "base").GetCondition(condv1alpha1.ConditionTypeReady)
	assert.False(t, ready.IsTrue())
	assert.Equal(t, string(condv1alpha1.ConditionReasonUnrecoverable), ready.Reason)
	assert.Equal(t, int64(3), ready.ObservedGeneration)
}

// A Config the transaction just wrote follows that outcome, even when the
// listed copy has not seen it yet: the write is not conditional on the list.
func TestUpdateConfigsReady_FollowsOutcome(t *testing.T) {
	ctx := context.Background()
	c := newFakeConfigClient(t, nil, newUnrecoverableConfig("base", 4))
	tr := NewTransactor(c)

	listed := listTargetConfigs(t, c)
	// the transaction writes ConfigReady after the list was taken
	cfg := getTargetConfig(t, c, "base")
	cfg.SetConditions(configv1alpha1.ConfigReady(4, ""))
	require.NoError(t, c.Status().Update(ctx, cfg))

	written := outcomes{"default.base": configv1alpha1.ConfigReady(4, "")}
	require.NoError(t, tr.UpdateConfigsReady(ctx, listed, readyFromOutcomes(written)))

	ready := getTargetConfig(t, c, "base").GetCondition(condv1alpha1.ConditionTypeReady)
	assert.True(t, ready.IsTrue(), "Ready follows the outcome, not the stale list")
	assert.Equal(t, int64(4), ready.ObservedGeneration)
}

// A target stage sets Ready with the Config's generation; ConfigReady is not
// touched.
func TestUpdateConfigsReady_NotReadyKeepsConfigReady(t *testing.T) {
	c := newFakeConfigClient(t, nil, newUnrecoverableConfig("base", 2))
	tr := NewTransactor(c)

	require.NoError(t, tr.UpdateConfigsReady(context.Background(), listTargetConfigs(t, c),
		NotReady(configv1alpha1.ConfigReasonTargetNotReady, "target down")))

	cfg := getTargetConfig(t, c, "base")
	ready := cfg.GetCondition(condv1alpha1.ConditionTypeReady)
	assert.Equal(t, string(configv1alpha1.ConfigReasonTargetNotReady), ready.Reason)
	assert.Equal(t, int64(2), ready.ObservedGeneration)
	assert.Equal(t, string(condv1alpha1.ConditionReasonUnrecoverable),
		cfg.GetCondition(configv1alpha1.ConditionTypeConfigReady).Reason)
}

// Ready decided on a stale list is not written (409 dropped): the newer
// status is already queued as an event and decided again.
func TestUpdateConfigsReady_StaleListDropped(t *testing.T) {
	ctx := context.Background()
	c := newFakeConfigClient(t, nil, newUnrecoverableConfig("base", 1))
	tr := NewTransactor(c)

	stale := listTargetConfigs(t, c)
	require.NoError(t, tr.UpdateConfigsReady(ctx, listTargetConfigs(t, c),
		NotReady(configv1alpha1.ConfigReasonTargetNotReady, "target down")))

	require.NoError(t, tr.UpdateConfigsReady(ctx, stale,
		NotReady(configv1alpha1.ConfigReasonTargetRuntimeNotReady, "runtime down")),
		"a conflict is not an error")

	assert.Equal(t, string(configv1alpha1.ConfigReasonTargetNotReady),
		getTargetConfig(t, c, "base").GetCondition(condv1alpha1.ConditionTypeReady).Reason,
		"the stale write must not land")
}

// A Config released (gone) since it was listed has nothing to report on: its
// Ready write is not an error.
func TestUpdateConfigsReady_ConfigGoneSinceListed(t *testing.T) {
	ctx := context.Background()
	c := newFakeConfigClient(t, nil, newUnrecoverableConfig("base", 1))
	tr := NewTransactor(c)

	listed := listTargetConfigs(t, c)
	require.NoError(t, c.Delete(ctx, getTargetConfig(t, c, "base")))

	require.NoError(t, tr.UpdateConfigsReady(ctx, listed,
		NotReady(configv1alpha1.ConfigReasonTargetNotFound, "target default/dev1 not found")))
}