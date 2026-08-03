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

	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAnalyzeIntentResponse(t *testing.T) {
	cases := map[string]struct {
		rsp              *sdcpb.TransactionSetResponse
		err              error
		expectErrors     bool
		recoverable      bool
		globalErrContain string
		intentErrContain string
		warnContain      string
	}{
		"Success_NoWarningsOrErrors": {
			rsp:          &sdcpb.TransactionSetResponse{},
			err:          nil,
			expectErrors: false,
		},
		"Recoverable gRPC Error": {
			rsp:              nil,
			err:              status.Error(codes.ResourceExhausted, "quota exceeded"),
			expectErrors:     true,
			recoverable:      true,
			globalErrContain: "quota exceeded",
		},
		"Non-Recoverable gRPC Error": {
			rsp:              nil,
			err:              status.Error(codes.InvalidArgument, "invalid request"),
			expectErrors:     true,
			recoverable:      false,
			globalErrContain: "invalid request",
		},
		"Intent Errors in Response": {
			rsp: &sdcpb.TransactionSetResponse{
				Intents: map[string]*sdcpb.TransactionSetResponseIntent{
					"intent1": {Errors: []string{"failed to apply update"}},
				},
			},
			err:              nil,
			expectErrors:     true,
			recoverable:      false,
			intentErrContain: "failed to apply update",
		},
		"Global Warnings Only": {
			rsp: &sdcpb.TransactionSetResponse{
				Warnings: []string{"slow transaction detected"},
			},
			err:          nil,
			expectErrors: false,
			warnContain:  "slow transaction detected",
		},
		"Intent Warnings Only": {
			rsp: &sdcpb.TransactionSetResponse{
				Intents: map[string]*sdcpb.TransactionSetResponseIntent{
					"intent1": {Warnings: []string{"potential inconsistency"}},
				},
			},
			err:          nil,
			expectErrors: false,
			warnContain:  "potential inconsistency",
		},
		"Both Errors and Warnings": {
			rsp: &sdcpb.TransactionSetResponse{
				Warnings: []string{"global slow transaction"},
				Intents: map[string]*sdcpb.TransactionSetResponseIntent{
					"intent1": {
						Warnings: []string{"intent warning"},
						Errors:   []string{"intent error"},
					},
				},
			},
			err:              nil,
			expectErrors:     true,
			recoverable:      false,
			intentErrContain: "intent error",
			warnContain:      "global slow transaction",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			result := AnalyzeIntentResponse(tc.err, tc.rsp)

			if tc.expectErrors {
				assert.True(t, result.HasErrors(), "expected HasErrors() == true")
				assert.Equal(t, tc.recoverable, result.Recoverable,
					"Recoverable mismatch")

				if tc.globalErrContain != "" {
					assert.NotNil(t, result.GlobalError)
					assert.Contains(t, result.GlobalError.Error(), tc.globalErrContain)
				}
				if tc.intentErrContain != "" {
					assert.NotNil(t, result.IntentErrors)
					assert.Contains(t, result.IntentErrors.Error(), tc.intentErrContain)
				}
			} else {
				assert.False(t, result.HasErrors(), "expected HasErrors() == false")
				assert.Nil(t, result.GlobalError)
				assert.Nil(t, result.IntentErrors)
			}

			if tc.warnContain != "" {
				found := false
				for _, w := range result.GlobalWarnings {
					if contains(w, tc.warnContain) {
						found = true
						break
					}
				}
				assert.True(t, found,
					"expected warning containing %q in %v", tc.warnContain, result.GlobalWarnings)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}