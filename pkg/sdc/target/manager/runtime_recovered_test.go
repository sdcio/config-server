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
	"fmt"
	"testing"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/stretchr/testify/assert"
)

func newTestRuntime() *TargetRuntime {
	return NewTargetRuntime(storebackend.ToKey("ns/target"), nil, nil)
}

// A dataserver Down->Up flap clears Recovered, but the connection snapshot
// pushed to the Target is identical before and after the flap. No Target
// status change means nothing re-triggers the recovery reconciler, so
// Recovered stays false while the Target keeps reporting Ready.
func TestRecoveredClearedByDSFlapIsInvisibleInConnSnapshot(t *testing.T) {
	ctx := context.Background()
	rt := newTestRuntime()

	rt.setDSReady(ctx, true, nil)
	rt.setDatastoreReady(true)
	rt.setPhase(ctx, PhaseRunning, nil)
	rt.setRecovered(true)

	connBefore, msgBefore := rt.connSnapshot()
	assert.True(t, connBefore)
	assert.True(t, rt.Status().Recovered)

	// flap: Down then Up, runtime re-ensures datastore and ends up Running again
	rt.setDSReady(ctx, false, fmt.Errorf("grpc state: TRANSIENT_FAILURE"))
	rt.setDSReady(ctx, true, nil)
	rt.setDatastoreReady(true)
	rt.setPhase(ctx, PhaseRunning, nil)

	connAfter, msgAfter := rt.connSnapshot()
	assert.Equal(t, connBefore, connAfter, "conn snapshot is unchanged -> no Target status write")
	assert.Equal(t, msgBefore, msgAfter)
	assert.False(t, rt.Status().Recovered, "Recovered was cleared and nobody is told to redo recovery")
}

// Recreating the datastore (hash change or NotFound) does not clear Recovered,
// so intents are not replayed into the fresh, empty datastore.
func TestRecoveredNotClearedWhenDatastoreRecreated(t *testing.T) {
	rt := newTestRuntime()
	rt.setDatastoreReady(true)
	rt.setRecovered(true)

	// ensureDatastore's create / recreate branches only call setDatastoreReady(true)
	rt.setDatastoreReady(true)

	assert.True(t, rt.Status().Recovered, "documents current behavior: recovered survives datastore recreation")
}
