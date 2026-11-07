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

package client

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetSchemaServerAddress(t *testing.T) {
	cases := map[string]struct {
		env      map[string]string
		expected string
	}{
		"Default": {
			expected: "schema-server.sdc-system.svc.cluster.local:56000",
		},
		"PodNamespace": {
			env:      map[string]string{"POD_NAMESPACE": "network-automation"},
			expected: "schema-server.network-automation.svc.cluster.local:56000",
		},
		"EmptyPodNamespace": {
			env:      map[string]string{"POD_NAMESPACE": ""},
			expected: "schema-server.sdc-system.svc.cluster.local:56000",
		},
		"ServiceAndPort": {
			env: map[string]string{
				"POD_NAMESPACE":             "network-automation",
				"SDC_SCHEMA_SERVER_SERVICE": "my-schema-server",
				"SDC_SCHEMA_SERVER_PORT":    "50051",
			},
			expected: "my-schema-server.network-automation.svc.cluster.local:50051",
		},
		"SchemaServerOverride": {
			env: map[string]string{
				"POD_NAMESPACE":     "network-automation",
				"SDC_SCHEMA_SERVER": "schema-server.other.svc.cluster.local:56000",
				"SDC_DATA_SERVER":   "data-server.other.svc.cluster.local:56000",
			},
			expected: "schema-server.other.svc.cluster.local:56000",
		},
		"DataServerOverride": {
			env: map[string]string{
				"POD_NAMESPACE":   "network-automation",
				"SDC_DATA_SERVER": "data-server.other.svc.cluster.local:56000",
			},
			expected: "data-server.other.svc.cluster.local:56000",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			for _, key := range []string{
				"SDC_SCHEMA_SERVER",
				"SDC_DATA_SERVER",
				"SDC_SCHEMA_SERVER_SERVICE",
				"SDC_SCHEMA_SERVER_PORT",
				"POD_NAMESPACE",
			} {
				unsetEnv(t, key)
			}
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			assert.Equal(t, tc.expected, GetSchemaServerAddress())
		})
	}
}

// unsetEnv removes key from the environment for the duration of the test,
// so the result does not depend on the environment the test runs in.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	// t.Setenv registers the restore of the original value on cleanup
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("cannot unset %s: %v", key, err)
	}
}
