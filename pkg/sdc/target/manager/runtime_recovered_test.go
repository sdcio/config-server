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
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newTestRuntime() *TargetRuntime {
	return NewTargetRuntime(storebackend.ToKey("ns/target"), nil, nil)
}

// A dataserver Down->Up flap clears Recovered while the connection snapshot is
// identical before and after, so the connection push alone never tells anyone to
// redo recovery. The recovery condition must follow the flag instead.
func TestRecoveredClearedByDSFlapDemotesRecoveryCondition(t *testing.T) {
	ctx := context.Background()
	rt := newTestRuntime()

	rt.setDSReady(ctx, true, nil)
	rt.setDatastoreReady(true)
	rt.setPhase(ctx, PhaseRunning, nil)
	rt.setRecovered(true)

	connBefore, msgBefore := rt.connSnapshot()
	assert.True(t, rt.Status().Recovered)

	rt.setDSReady(ctx, false, fmt.Errorf("grpc state: TRANSIENT_FAILURE"))
	rt.setDSReady(ctx, true, nil)
	rt.setDatastoreReady(true)
	rt.setPhase(ctx, PhaseRunning, nil)

	connAfter, msgAfter := rt.connSnapshot()
	assert.Equal(t, connBefore, connAfter)
	assert.Equal(t, msgBefore, msgAfter)
	assert.False(t, rt.Status().Recovered)

	cond, apply := recoveryConditionFor(rt.Status().Recovered, configv1alpha1.TargetConfigRecoveryReady(""))
	assert.True(t, apply, "a True recovery condition must not outlive Recovered=false")
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, msgConfigNotRecovered, cond.Message)
}

func TestRecoveryConditionFor(t *testing.T) {
	ready := configv1alpha1.TargetConfigRecoveryReady("")
	failed := configv1alpha1.TargetConfigRecoveryFailed("setIntent failed")

	_, apply := recoveryConditionFor(true, ready)
	assert.False(t, apply, "recovered: the recovery reconciler owns promoting the condition")
	_, apply = recoveryConditionFor(true, failed)
	assert.False(t, apply)
	_, apply = recoveryConditionFor(false, failed)
	assert.False(t, apply, "an existing failure keeps its more specific message")
	_, apply = recoveryConditionFor(false, condv1alpha1.Condition{})
	assert.False(t, apply, "no condition yet: the recovery reconciler sets the first one")
}

// fakeDS lets a test script GetDataStore on top of the no-op fake client.
type fakeDS struct {
	dsclient.Client
	getErr  error
	created int
}

func (f *fakeDS) GetDataStore(ctx context.Context, in *sdcpb.GetDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.GetDataStoreResponse, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.Client.GetDataStore(ctx, in, opts...)
}

func (f *fakeDS) CreateDataStore(ctx context.Context, in *sdcpb.CreateDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.CreateDataStoreResponse, error) {
	f.created++
	return f.Client.CreateDataStore(ctx, in, opts...)
}

// Recreating the datastore (hash change or NotFound) clears Recovered so the
// intents are replayed into the fresh, empty datastore.
func TestRecoveredClearedWhenDatastoreRecreatedOnHashChange(t *testing.T) {
	rt := newTestRuntime()
	rt.SetDesired(&sdcpb.CreateDataStoreRequest{}, nil, "new")
	rt.setRunningHash("old")
	rt.setDatastoreReady(true)
	rt.setRecovered(true)

	ds := &fakeDS{Client: dsclient.NewFakeClient()}
	assert.NoError(t, rt.ensureDatastore(context.Background(), ds, rt.getDesired()))

	assert.Equal(t, 1, ds.created)
	assert.False(t, rt.Status().Recovered)
	assert.True(t, rt.Status().DSStoreReady)
}

func TestRecoveredClearedWhenDatastoreCreatedAfterNotFound(t *testing.T) {
	rt := newTestRuntime()
	rt.SetDesired(&sdcpb.CreateDataStoreRequest{}, nil, "h")
	rt.setRunningHash("h")
	rt.setDatastoreReady(true)
	rt.setRecovered(true)

	ds := &fakeDS{Client: dsclient.NewFakeClient(), getErr: status.Error(codes.NotFound, "unknown datastore")}
	assert.NoError(t, rt.ensureDatastore(context.Background(), ds, rt.getDesired()))

	assert.Equal(t, 1, ds.created)
	assert.False(t, rt.Status().Recovered)
}

func TestRecoveredKeptWhenDatastoreUnchanged(t *testing.T) {
	rt := newTestRuntime()
	rt.SetDesired(&sdcpb.CreateDataStoreRequest{}, nil, "h")
	rt.setRunningHash("h")
	rt.setDatastoreReady(true)
	rt.setRecovered(true)

	ds := &fakeDS{Client: dsclient.NewFakeClient()}
	assert.NoError(t, rt.ensureDatastore(context.Background(), ds, rt.getDesired()))

	assert.True(t, rt.Status().Recovered)
}
