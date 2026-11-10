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
	"fmt"
	"sync"
	"testing"
	"time"

	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeDS models the data-server transaction slot: one open transaction per
// datastore, errors as the data-server returns them (plain errors arrive as
// codes.Unknown).
type fakeDS struct {
	mu   sync.Mutex
	slot string // id of the open transaction, "" if none

	// setFn decides the outcome of a TransactionSet. register: the
	// data-server took the slot (also when an error is returned).
	setFn func(req *sdcpb.TransactionSetRequest) (rsp *sdcpb.TransactionSetResponse, err error, register bool)
	// queued errors, popped per call; nil entry = normal behaviour
	confirmErrs []error
	cancelErrs  []error

	sets, confirms, cancels []string
	afterSet                func() // hook, runs after a Set returned
}

func unknownErr(msg string) error { return status.Error(codes.Unknown, msg) }

func okSet(*sdcpb.TransactionSetRequest) (*sdcpb.TransactionSetResponse, error, bool) {
	return &sdcpb.TransactionSetResponse{}, nil, true
}

func (f *fakeDS) TransactionSet(ctx context.Context, in *sdcpb.TransactionSetRequest, _ ...grpc.CallOption) (*sdcpb.TransactionSetResponse, error) {
	f.mu.Lock()
	defer func() {
		f.mu.Unlock()
		if f.afterSet != nil {
			f.afterSet()
		}
	}()
	f.sets = append(f.sets, in.GetTransactionId())
	if in.GetDryRun() {
		return &sdcpb.TransactionSetResponse{}, nil
	}
	if f.slot != "" {
		return nil, unknownErr("transaction ongoing")
	}
	fn := f.setFn
	if fn == nil {
		fn = okSet
	}
	rsp, err, register := fn(in)
	if register {
		f.slot = in.GetTransactionId()
	}
	return rsp, err
}

func pop(errs *[]error) (error, bool) {
	if len(*errs) == 0 {
		return nil, false
	}
	e := (*errs)[0]
	*errs = (*errs)[1:]
	return e, e != nil
}

func (f *fakeDS) closeSlot(id string) error {
	switch {
	case f.slot == "":
		return unknownErr("no ongoing transaction")
	case f.slot != id:
		return unknownErr(fmt.Sprintf("transaction id %s is invalid", id))
	}
	f.slot = ""
	return nil
}

func (f *fakeDS) TransactionConfirm(_ context.Context, in *sdcpb.TransactionConfirmRequest, _ ...grpc.CallOption) (*sdcpb.TransactionConfirmResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.confirms = append(f.confirms, in.GetTransactionId())
	if err, ok := pop(&f.confirmErrs); ok {
		return nil, err
	}
	return nil, f.closeSlot(in.GetTransactionId())
}

func (f *fakeDS) TransactionCancel(_ context.Context, in *sdcpb.TransactionCancelRequest, _ ...grpc.CallOption) (*sdcpb.TransactionCancelResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancels = append(f.cancels, in.GetTransactionId())
	if err, ok := pop(&f.cancelErrs); ok {
		return nil, err
	}
	return nil, f.closeSlot(in.GetTransactionId())
}

func txReq(id string) *sdcpb.TransactionSetRequest {
	return &sdcpb.TransactionSetRequest{
		TransactionId: id,
		DatastoreName: "default.dev1",
		Intents:       []*sdcpb.TransactionIntent{{Intent: "cfg1"}},
	}
}

func requireRecoverable(t *testing.T, err error) {
	t.Helper()
	var txErr *TransactionError
	if !errors.As(err, &txErr) || !txErr.Recoverable {
		t.Fatalf("want recoverable TransactionError, got %v", err)
	}
}

func TestCommitHappyPath(t *testing.T) {
	ds := &fakeDS{}
	g := NewTxGate()
	res := commitTransaction(context.Background(), g, ds, txReq("t1"))
	if !res.committed() {
		t.Fatalf("not committed: %+v", res)
	}
	if ds.slot != "" || len(g.pending) != 0 || len(ds.cancels) != 0 {
		t.Fatalf("slot=%q pending=%v cancels=%v", ds.slot, g.pending, ds.cancels)
	}
}

func TestCommitConfirmRetriesWhileLocked(t *testing.T) {
	locked := status.Error(codes.Aborted, "datastore is locked, other action is ongoing")
	ds := &fakeDS{confirmErrs: []error{locked, locked}}
	g := NewTxGate()
	res := commitTransaction(context.Background(), g, ds, txReq("t1"))
	if !res.committed() {
		t.Fatalf("not committed: %+v", res)
	}
	if len(ds.confirms) != 3 || ds.slot != "" {
		t.Fatalf("confirms=%v slot=%q", ds.confirms, ds.slot)
	}
}

func TestCommitConfirmFailsIsCancelled(t *testing.T) {
	// e.g. a confirm the data-server rejects for good
	ds := &fakeDS{confirmErrs: []error{unknownErr("boom")}}
	g := NewTxGate()
	res := commitTransaction(context.Background(), g, ds, txReq("t1"))
	requireRecoverable(t, res.Err)
	if ds.slot != "" || len(g.pending) != 0 || len(ds.cancels) != 1 {
		t.Fatalf("slot=%q pending=%v cancels=%v", ds.slot, g.pending, ds.cancels)
	}
}

func TestCommitUnresolvedCancelIsRetriedBeforeNextTransaction(t *testing.T) {
	unavailable := status.Error(codes.Unavailable, "connection reset")
	ds := &fakeDS{
		confirmErrs: []error{unknownErr("boom")},
		cancelErrs:  []error{unavailable},
	}
	g := NewTxGate()

	res := commitTransaction(context.Background(), g, ds, txReq("t1"))
	requireRecoverable(t, res.Err)
	if ds.slot != "t1" || len(g.pending) != 1 {
		t.Fatalf("expected t1 still open and pending: slot=%q pending=%v", ds.slot, g.pending)
	}

	// next transaction first cancels t1, then commits
	res = commitTransaction(context.Background(), g, ds, txReq("t2"))
	if !res.committed() {
		t.Fatalf("t2 not committed: %+v", res)
	}
	if ds.slot != "" || len(g.pending) != 0 {
		t.Fatalf("slot=%q pending=%v", ds.slot, g.pending)
	}
	if got := ds.cancels; len(got) != 2 || got[1] != "t1" {
		t.Fatalf("cancels=%v", got)
	}
}

func TestCommitPendingCancelStillFailingBlocksNewSet(t *testing.T) {
	unavailable := status.Error(codes.Unavailable, "connection reset")
	ds := &fakeDS{
		confirmErrs: []error{unknownErr("boom")},
		cancelErrs:  []error{unavailable, unavailable},
	}
	g := NewTxGate()
	_ = commitTransaction(context.Background(), g, ds, txReq("t1"))

	res := commitTransaction(context.Background(), g, ds, txReq("t2"))
	requireRecoverable(t, res.Err)
	if len(ds.sets) != 1 {
		t.Fatalf("t2 must not be sent while t1 is open: sets=%v", ds.sets)
	}
}

func TestCommitAmbiguousSetErrorIsCancelled(t *testing.T) {
	// the set landed, but the client only saw an error
	ds := &fakeDS{setFn: func(*sdcpb.TransactionSetRequest) (*sdcpb.TransactionSetResponse, error, bool) {
		return nil, status.Error(codes.Unavailable, "too_many_pings"), true
	}}
	g := NewTxGate()
	res := commitTransaction(context.Background(), g, ds, txReq("t1"))
	if res.SetErr == nil || res.committed() {
		t.Fatalf("want set error: %+v", res)
	}
	if ds.slot != "" || len(g.pending) != 0 {
		t.Fatalf("slot=%q pending=%v", ds.slot, g.pending)
	}
}

func TestCommitOngoingForeignTransactionIsNotCancelled(t *testing.T) {
	ds := &fakeDS{slot: "someone-else"}
	g := NewTxGate()
	res := commitTransaction(context.Background(), g, ds, txReq("t1"))
	if res.SetErr == nil {
		t.Fatal("want transaction ongoing")
	}
	if !isRecoverableTransactionError(res.SetErr) {
		t.Fatalf("transaction ongoing must be recoverable: %v", res.SetErr)
	}
	// data-server Cancel rolls back whatever is open before checking the
	// id: never send one for a transaction that is not ours
	if len(ds.cancels) != 0 || len(g.pending) != 0 {
		t.Fatalf("cancels=%v pending=%v", ds.cancels, g.pending)
	}
}

func TestCommitValidationErrorIsCancelledDefensively(t *testing.T) {
	// a data-server that keeps the slot after a validation failure
	ds := &fakeDS{setFn: func(*sdcpb.TransactionSetRequest) (*sdcpb.TransactionSetResponse, error, bool) {
		return &sdcpb.TransactionSetResponse{Intents: map[string]*sdcpb.TransactionSetResponseIntent{
			"cfg1": {Errors: []string{"leaf out of range"}},
		}}, nil, true
	}}
	g := NewTxGate()
	res := commitTransaction(context.Background(), g, ds, txReq("t1"))
	if res.committed() || res.Err != nil || res.SetErr != nil {
		t.Fatalf("want intent errors only: %+v", res)
	}
	if ds.slot != "" || len(g.pending) != 0 || len(ds.confirms) != 0 {
		t.Fatalf("slot=%q pending=%v confirms=%v", ds.slot, g.pending, ds.confirms)
	}
}

func TestCommitEmptyTransactionIsNeverSent(t *testing.T) {
	ds := &fakeDS{}
	req := txReq("t1")
	req.Intents = nil
	res := commitTransaction(context.Background(), NewTxGate(), ds, req)
	if !res.committed() || len(ds.sets) != 0 {
		t.Fatalf("res=%+v sets=%v", res, ds.sets)
	}
}

func TestCommitConfirmsAfterCallerCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ds := &fakeDS{afterSet: cancel} // caller goes away right after the set
	res := commitTransaction(ctx, NewTxGate(), ds, txReq("t1"))
	if !res.committed() || ds.slot != "" {
		t.Fatalf("res=%+v slot=%q", res, ds.slot)
	}
}

func TestCommitGateSerializes(t *testing.T) {
	ds := &fakeDS{}
	g := NewTxGate()
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := commitTransaction(context.Background(), g, ds, txReq(fmt.Sprintf("t%d", i)))
			if !res.committed() {
				errs <- fmt.Errorf("t%d: set=%v err=%v", i, res.SetErr, res.Err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestCommitDropsStalePending(t *testing.T) {
	ds := &fakeDS{}
	g := NewTxGate()
	g.pending["old"] = time.Now().Add(-2 * txPendingMaxAge)
	res := commitTransaction(context.Background(), g, ds, txReq("t1"))
	if !res.committed() || len(ds.cancels) != 0 || len(g.pending) != 0 {
		t.Fatalf("res=%+v cancels=%v pending=%v", res, ds.cancels, g.pending)
	}
}

func TestTxErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		err                         error
		transient, notOpen, refused bool
	}{
		{unknownErr("transaction ongoing"), true, false, true},
		{status.Error(codes.Aborted, "datastore is locked, other action is ongoing"), true, false, true},
		{unknownErr("context is closed (done)"), true, false, false},
		{status.Error(codes.Unavailable, "x"), true, false, false},
		{unknownErr("no ongoing transaction"), false, true, false},
		{unknownErr("transaction id abc is invalid"), false, true, false},
		{unknownErr("leaf value is invalid"), false, false, false},
		{status.Error(codes.NotFound, "unknown datastore x"), false, true, true},
		{unknownErr("boom"), false, false, false},
	} {
		if got := isTransientTxErr(tc.err); got != tc.transient {
			t.Errorf("%v: transient=%v", tc.err, got)
		}
		if got := isTxNotOpenErr(tc.err); got != tc.notOpen {
			t.Errorf("%v: notOpen=%v", tc.err, got)
		}
		if got := setNotRegistered(tc.err); got != tc.refused {
			t.Errorf("%v: refused=%v", tc.err, got)
		}
	}
}