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

package config

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/sdcio/config-server/apis/condition"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func unrecoverableConfig(t *testing.T, rv string) *Config {
	t.Helper()
	msg, err := json.Marshal(condition.UnrecoverableMessage{ResourceVersion: rv, Message: "boom"})
	assert.NoError(t, err)

	cfg := &Config{}
	cfg.SetResourceVersion(rv)
	cfg.SetConditions(condition.Condition{Condition: metav1.Condition{
		Type:    string(ConditionTypeConfigReady),
		Status:  metav1.ConditionFalse,
		Reason:  string(condition.ConditionReasonUnrecoverable),
		Message: string(msg),
	}})
	return cfg
}

func TestIsRecoverableSameResourceVersion(t *testing.T) {
	cfg := unrecoverableConfig(t, "7")
	assert.False(t, cfg.IsRecoverable(context.Background()))
}

// The transactor stores the resourceVersion read before the status write,
// and every status write bumps resourceVersion. The stored version is stale
// as soon as the write lands, so the config is retried instead of parked.
func TestIsRecoverableAfterStatusWriteBumpsResourceVersion(t *testing.T) {
	cfg := unrecoverableConfig(t, "7")

	n, err := strconv.Atoi(cfg.GetResourceVersion())
	assert.NoError(t, err)
	cfg.SetResourceVersion(strconv.Itoa(n + 1))

	assert.True(t, cfg.IsRecoverable(context.Background()),
		"documents current behavior: unrecoverable marker is invalidated by its own write")
}
