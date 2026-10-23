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
	"testing"

	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/stretchr/testify/assert"
)

// Recovery passes a ConfigReady-typed condition as the "target" condition.
// The applied condition list must still carry one entry per type.
func TestTargetStatusConditions_NoDuplicateTypesForConfigReady(t *testing.T) {
	cases := map[string]condv1alpha1.Condition{
		"recovered": configv1alpha1.ConfigReady("target recovered"),
		"failed":    configv1alpha1.ConfigFailed("recovery failed"),
	}
	for name, targetCond := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := &configv1alpha1.Config{}
			cfg.SetConditions(targetCond)
			cfg.SetOverallStatus()
			configCond := cfg.GetCondition(condv1alpha1.ConditionType(configv1alpha1.ConfigReady("").Type))
			ready := cfg.GetCondition(condv1alpha1.ConditionTypeReady)

			got := targetStatusConditions(targetCond, configCond, ready)

			seen := map[string]int{}
			for _, c := range got {
				seen[c.Type]++
			}
			for typ, n := range seen {
				assert.Equal(t, 1, n, "duplicate condition type %s", typ)
			}
			assert.Equal(t, targetCond.Message, findCond(got, targetCond.Type).Message)
		})
	}
}

func TestTargetStatusConditions_DistinctTypesKept(t *testing.T) {
	got := targetStatusConditions(
		configv1alpha1.TargetForConfigReady("target ready"),
		configv1alpha1.ConfigReady("applied"),
		condv1alpha1.ReadyWithMsg("applied"),
	)
	assert.Len(t, got, 3)
}

func findCond(conds []condv1alpha1.Condition, typ string) condv1alpha1.Condition {
	for _, c := range conds {
		if c.Type == typ {
			return c
		}
	}
	return condv1alpha1.Condition{}
}
