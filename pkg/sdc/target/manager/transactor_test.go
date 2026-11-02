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
	"strings"
	"testing"

	"github.com/sdcio/config-server/apis/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestBuildGRPCIntents_usesResolvedBlobs(t *testing.T) {
	cfg := &config.Config{}
	cfg.Spec.Config = []config.ConfigBlob{
		{
			Path: "/interface[name=ethernet-1/4]",
			Value: runtime.RawExtension{
				Raw: []byte(`{"description":"${vars.e11desc}"}`),
			},
		},
	}

	resolved := []config.ConfigBlob{
		{
			Path: "/interface[name=ethernet-1/4]",
			Value: runtime.RawExtension{
				Raw: []byte(`{"description":"resolved-secret-value"}`),
			},
		},
	}

	intents, err := BuildGRPCIntents([]IntentInput{{
		Config:        cfg,
		ResolvedBlobs: resolved,
	}}, nil)
	require.NoError(t, err)
	require.Len(t, intents, 1)
	require.Len(t, intents[0].Update, 1)

	got := string(intents[0].Update[0].Value.GetJsonVal())
	assert.True(t, strings.Contains(got, "resolved-secret-value"),
		"intent must use ResolvedBlobs, got %q", got)
	assert.False(t, strings.Contains(got, "${vars."),
		"intent must not ship unresolved placeholders, got %q", got)
}
