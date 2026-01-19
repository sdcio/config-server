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

package target

import (
	"context"
	"errors"
	"testing"

	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestProcessTransactionResponse(t *testing.T) {
	cases := map[string]struct {
		rsp         *sdcpb.TransactionSetResponse
		err         error
		expected    string
		expectErr   bool
		recoverable bool
	}{
		"Success_NoWarningsOrErrors": {
			rsp:       &sdcpb.TransactionSetResponse{},
			err:       nil,
			expected:  "",
			expectErr: false,
		},
		"Recoverable gRPC Error": {
			rsp:         nil,
			err:         status.Error(codes.ResourceExhausted, "quota exceeded"),
			expected:    "error: rpc error: code = ResourceExhausted desc = quota exceeded",
			expectErr:   true,
			recoverable: true,
		},
		"Non-Recoverable gRPC Error": {
			rsp:         nil,
			err:         status.Error(codes.InvalidArgument, "invalid request"),
			expected:    "error: rpc error: code = InvalidArgument desc = invalid request",
			expectErr:   true,
			recoverable: false,
		},
		"Intent Errors in Response": {
			rsp: &sdcpb.TransactionSetResponse{
				Intents: map[string]*sdcpb.TransactionSetResponseIntent{
					"intent1": {Errors: []string{"failed to apply update"}},
				},
			},
			err:       nil,
			expected:  "intent \"intent1\" error: \"failed to apply update\"",
			expectErr: true,
		},
		"Global Warnings Only": {
			rsp: &sdcpb.TransactionSetResponse{
				Warnings: []string{"slow transaction detected"},
			},
			err:       nil,
			expected:  "global warning: \"slow transaction detected\"",
			expectErr: false,
		},
		"Intent Warnings Only": {
			rsp: &sdcpb.TransactionSetResponse{
				Intents: map[string]*sdcpb.TransactionSetResponseIntent{
					"intent1": {Warnings: []string{"potential inconsistency"}},
				},
			},
			err:       nil,
			expected:  "intent \"intent1\" warning: \"potential inconsistency\"",
			expectErr: false,
		},
		"Both Errors and Warnings": {
			rsp: &sdcpb.TransactionSetResponse{
				Warnings: []string{"global slow transaction"},
				Intents: map[string]*sdcpb.TransactionSetResponseIntent{
					"intent1": {Warnings: []string{"intent warning"}, Errors: []string{"intent error"}},
				},
			},
			err:       nil,
			expected:  "intent \"intent1\" error: \"intent error\"",
			expectErr: true,
		},
	}

	ctx := context.Background()
	//key := storebackend.ToKey("dummy")
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			warning, err := processTransactionResponse(ctx, tc.rsp, tc.err)

			if tc.expectErr {
				assert.Error(t, err, "expected an error but got none")
				var txErr *TransactionError
				assert.True(t, errors.As(err, &txErr))
				if tc.recoverable {
					assert.True(t, txErr.Recoverable, "Expected recoverable error")
				} else {
					assert.False(t, txErr.Recoverable, "Expected non-recoverable error")
				}
				assert.Contains(t, err.Error(), tc.expected)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, warning)
			}
		})
	}
}
