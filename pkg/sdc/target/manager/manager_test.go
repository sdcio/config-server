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
	"errors"
	"testing"
	"time"

	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeDeleter struct {
	err    error
	called int
	gotCtx context.Context
	name   string
}

func (f *fakeDeleter) DeleteDataStore(ctx context.Context, in *sdcpb.DeleteDataStoreRequest, _ ...grpc.CallOption) (*sdcpb.DeleteDataStoreResponse, error) {
	f.called++
	f.gotCtx = ctx
	f.name = in.GetName()
	return &sdcpb.DeleteDataStoreResponse{}, f.err
}

// Regression coverage for sdcio/config-server#516.
func TestDeleteDatastore(t *testing.T) {
	cases := map[string]struct {
		err     error
		wantErr bool
	}{
		"deleted":                {err: nil, wantErr: false},
		"already absent (grpc)":  {err: status.Error(codes.NotFound, "missing"), wantErr: false},
		"already absent (text)":  {err: errors.New("unknown datastore default/dev1"), wantErr: false},
		"rpc failure keeps fin.": {err: status.Error(codes.Internal, "boom"), wantErr: true},
		"timeout keeps fin.":     {err: status.Error(codes.DeadlineExceeded, "timeout"), wantErr: true},
		"canceled keeps fin.":    {err: status.Error(codes.Canceled, "context canceled"), wantErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeDeleter{err: tc.err}
			err := deleteDatastore(context.Background(), f, "default/dev1")
			assert.Equal(t, 1, f.called)
			assert.Equal(t, "default/dev1", f.name)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// The delete RPC must run on a bounded context.
func TestDeleteDatastoreBoundedContext(t *testing.T) {
	f := &fakeDeleter{}
	assert.NoError(t, deleteDatastore(context.Background(), f, "default/dev1"))
	deadline, ok := f.gotCtx.Deadline()
	assert.True(t, ok, "delete must have a deadline")
	assert.WithinDuration(t, time.Now().Add(datastoreDeleteTimeout), deadline, time.Second)
}

// Dataserver unavailable: deletion fails, so the finalizer is retained.
// Also covers "no in-memory runtime": DeleteDatastore does not need one.
func TestManagerDeleteDatastoreWithoutDataserver(t *testing.T) {
	m := NewTargetManager(nil, nil)
	assert.Error(t, m.DeleteDatastore(context.Background(), testTargetKey()))
}

// Delete without a runtime (e.g. after restart) is a no-op success.
func TestManagerDeleteWithoutRuntime(t *testing.T) {
	m := NewTargetManager(nil, nil)
	assert.NoError(t, m.Delete(context.Background(), testTargetKey()))
}

// A runtime that does not stop in time stays registered, so the retry waits
// for the same runtime instead of racing it.
func TestManagerDeleteKeepsRuntimeUntilStopped(t *testing.T) {
	m := NewTargetManager(nil, nil)
	key := testTargetKey()
	rt := m.GetOrCreate(key)
	done := make(chan struct{})
	rt.cancel = func() {}
	rt.done = done

	// shorten the wait for the test
	errCh := make(chan error, 1)
	go func() { errCh <- m.Delete(context.Background(), key) }()
	time.Sleep(50 * time.Millisecond)
	assert.NotNil(t, m.GetRuntimeForTarget(context.Background(), key), "runtime must stay registered while stopping")

	close(done)
	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(runtimeStopTimeout + time.Second):
		t.Fatal("Delete did not return after the run loop exited")
	}
	assert.Nil(t, m.GetRuntimeForTarget(context.Background(), key))
}
