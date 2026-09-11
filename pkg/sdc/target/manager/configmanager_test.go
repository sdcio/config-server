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

	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProcessErrors_UnknownIntentFoldsToGlobal verifies that when data-server
// returns an intent key that is not a known Config CR (e.g. the reserved
// "running" key used for the synced device config), ProcessErrors folds its
// errors into the global error rather than declaring a dataServerError that
// would busy-loop via FailedUnRecoverable status updates.
func TestProcessErrors_UnknownIntentFoldsToGlobal(t *testing.T) {
	ctx := context.Background()

	// ConfigManager with no k8s client: safe because processFailedInput is
	// never invoked when both intent slices are empty.
	m := &ConfigManager{}

	rsp := &sdcpb.TransactionSetResponse{
		Intents: map[string]*sdcpb.TransactionSetResponseIntent{
			"running": {Errors: []string{"mandatory child [router-id] does not exist"}},
		},
	}

	retry, err := m.ProcessErrors(
		ctx,
		rsp,
		nil,   // no known update-intents
		nil,   // no known delete-intents
		nil,   // no pre-existing global error
		false,
	)

	require.Error(t, err, "expected an error from the folded 'running' intent")
	assert.False(t, retry)

	// The intent's own error message must be surfaced.
	assert.Contains(t, err.Error(), "mandatory child [router-id] does not exist",
		"intent error must be preserved in the returned error")

	// Must NOT declare a dataServerError — that message triggers the busy-loop.
	assert.NotContains(t, err.Error(), "dataserver reported",
		"'running' must not be treated as an unknown/rogue dataserver error")
}

// TestProcessErrors_MultipleUnknownIntentsFolded verifies that when multiple
// unknown keys are present, ALL of their errors are accumulated into globalErr
// (the old break-on-first-unknown semantics would have dropped subsequent ones).
func TestProcessErrors_MultipleUnknownIntentsFolded(t *testing.T) {
	ctx := context.Background()
	m := &ConfigManager{}

	rsp := &sdcpb.TransactionSetResponse{
		Intents: map[string]*sdcpb.TransactionSetResponseIntent{
			"running":   {Errors: []string{"bgp mandatory field missing"}},
			"__other__": {Errors: []string{"some other reserved error"}},
		},
	}

	retry, err := m.ProcessErrors(
		ctx,
		rsp,
		nil,
		nil,
		nil,
		false,
	)

	require.Error(t, err)
	assert.False(t, retry)
	assert.Contains(t, err.Error(), "bgp mandatory field missing")
	assert.Contains(t, err.Error(), "some other reserved error")
	assert.NotContains(t, err.Error(), "dataserver reported")
}
