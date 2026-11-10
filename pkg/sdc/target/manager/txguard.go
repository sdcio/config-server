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
	"sort"
	"strings"
	"time"

	"github.com/henderiw/logger/log"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The data-server keeps at most ONE open transaction per datastore. A
// transaction opened by a committing TransactionSet holds that slot until it
// is confirmed, cancelled, or the data-server rollback timer fires. While it
// is held, every other TransactionSet on the datastore fails.
//
// config-server therefore follows one protocol for every committing
// transaction, independent of how the data-server behaves:
//
//  1. take the per-datastore TxGate (recovery and the targetconfig
//     controller never overlap on one datastore);
//  2. cancel any transaction this process opened earlier and could not close;
//  3. TransactionSet, bounded by a deadline;
//  4. on success: TransactionConfirm, retried on transient errors;
//  5. on anything else: TransactionCancel of our own id on a detached
//     context. If that does not resolve, the id stays pending and step 2
//     cancels it before the next transaction.
//
// Every failure outside the Set validation result is recoverable: the caller
// retries with backoff instead of latching the Config or the Target.

const (
	// txGateWait bounds the wait for another transaction on the same datastore.
	txGateWait = 30 * time.Second
	// txSetRPCTimeout bounds TransactionSet (it includes the device commit).
	txSetRPCTimeout = 5 * time.Minute
	// txConfirmBudget bounds all confirm attempts together. It must stay well
	// below the shortest rollback timeout we send (60s), else the data-server
	// rolls back while we still try to confirm.
	txConfirmBudget = 30 * time.Second
	// txRPCTimeout bounds one Confirm or Cancel call.
	txRPCTimeout        = 10 * time.Second
	txConfirmBackoffMin = 250 * time.Millisecond
	txConfirmBackoffMax = 4 * time.Second
	// txPendingMaxAge drops a pending id we keep failing to cancel. It is
	// longer than any rollback timeout we send (and the data-server default
	// of 5m), so by then the data-server has released it on its own.
	txPendingMaxAge = 10 * time.Minute
)

// txClient is the part of the dataserver client the transaction protocol needs.
type txClient interface {
	TransactionSet(ctx context.Context, in *sdcpb.TransactionSetRequest, opts ...grpc.CallOption) (*sdcpb.TransactionSetResponse, error)
	TransactionConfirm(ctx context.Context, in *sdcpb.TransactionConfirmRequest, opts ...grpc.CallOption) (*sdcpb.TransactionConfirmResponse, error)
	TransactionCancel(ctx context.Context, in *sdcpb.TransactionCancelRequest, opts ...grpc.CallOption) (*sdcpb.TransactionCancelResponse, error)
}

// TxGate serializes committing transactions towards one datastore and
// remembers the transactions this process opened but could not close.
// One TxGate exists per target key and is shared by the target runtime
// (recovery) and every DatastoreHandle handed out for that target.
type TxGate struct {
	sem chan struct{}
	// pending maps a transaction id to the time it was opened. Only accessed
	// while holding sem.
	pending map[string]time.Time
	now     func() time.Time // for tests
}

func NewTxGate() *TxGate {
	return &TxGate{
		sem:     make(chan struct{}, 1),
		pending: map[string]time.Time{},
		now:     time.Now,
	}
}

func (g *TxGate) acquire(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, txGateWait)
	defer cancel()
	select {
	case g.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return NewTransactionError(
			fmt.Errorf("another transaction on this datastore is still in progress: %w", ctx.Err()), true)
	}
}

func (g *TxGate) release() { <-g.sem }

func (g *TxGate) pendingIDs() []string {
	ids := make([]string, 0, len(g.pending))
	for id := range g.pending {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// commitResult is the outcome of commitTransaction.
type commitResult struct {
	// Rsp and SetErr are what TransactionSet returned. Callers analyze them
	// exactly as before (intent errors, recoverability of SetErr).
	Rsp    *sdcpb.TransactionSetResponse
	SetErr error
	// Err is set when the transaction did not commit for a reason outside
	// the Set result: gate wait, a leftover transaction that could not be
	// cancelled, or a failed confirm. It is always a recoverable
	// *TransactionError.
	Err error
}

// committed reports whether the transaction was set and confirmed.
func (c commitResult) committed() bool {
	return c.SetErr == nil && c.Err == nil && !hasIntentErrors(c.Rsp)
}

// commitTransaction runs one committing (non dry-run) transaction under the
// datastore's gate. See the protocol at the top of this file.
func commitTransaction(ctx context.Context, gate *TxGate, cl txClient, req *sdcpb.TransactionSetRequest) commitResult {
	log := log.FromContext(ctx).With("datastore", req.GetDatastoreName(), "transactionID", req.GetTransactionId())

	if req.GetDryRun() {
		// dry-runs do not take the datastore slot
		rsp, err := cl.TransactionSet(ctx, req)
		return commitResult{Rsp: rsp, SetErr: err}
	}
	if len(req.GetIntents()) == 0 && req.GetReplaceIntent() == nil {
		// The data-server registers an empty transaction but never arms its
		// rollback timer for it: unless confirmed it would hold the datastore
		// forever. Never send one.
		return commitResult{Rsp: &sdcpb.TransactionSetResponse{}}
	}
	if gate == nil {
		gate = NewTxGate() // not shared; still gives confirm/cancel semantics
	}

	if err := gate.acquire(ctx); err != nil {
		return commitResult{Err: err}
	}
	defer gate.release()

	if err := cancelPending(ctx, gate, cl, req.GetDatastoreName()); err != nil {
		return commitResult{Err: err}
	}

	// Record the id BEFORE sending: the Set can land in the data-server even
	// when we only see an error.
	id := req.GetTransactionId()
	gate.pending[id] = gate.now()

	setCtx, cancel := context.WithTimeout(ctx, txSetRPCTimeout)
	rsp, setErr := cl.TransactionSet(setCtx, req)
	cancel()

	res := commitResult{Rsp: rsp, SetErr: setErr}

	switch {
	case setErr == nil && !hasIntentErrors(rsp):
		if err := confirmWithRetry(ctx, cl, req.GetDatastoreName(), id); err != nil {
			log.Warn("transaction confirm failed, cancelling", "err", err)
			cancelOwn(ctx, gate, cl, req.GetDatastoreName(), id)
			res.Err = NewTransactionError(fmt.Errorf("transaction confirm failed: %w", err), true)
			return res
		}
		delete(gate.pending, id)
		return res

	case setErr != nil && setNotRegistered(setErr):
		// The data-server refused before registering our transaction: there
		// is nothing of ours to cancel. Cancelling here could hit another
		// open transaction (data-server Cancel does not check the id before
		// rolling back).
		delete(gate.pending, id)
		return res

	default:
		// Validation failure (intent errors) or an ambiguous error: the
		// data-server should have discarded the transaction, but make sure.
		cancelOwn(ctx, gate, cl, req.GetDatastoreName(), id)
		return res
	}
}

// confirmWithRetry confirms the transaction, retrying transient errors within
// txConfirmBudget. It runs on a detached context: once the Set went through,
// a cancelled caller must not leave the transaction open.
func confirmWithRetry(ctx context.Context, cl txClient, datastore, id string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), txConfirmBudget)
	defer cancel()

	backoff := txConfirmBackoffMin
	for {
		callCtx, callCancel := context.WithTimeout(ctx, txRPCTimeout)
		_, err := cl.TransactionConfirm(callCtx, &sdcpb.TransactionConfirmRequest{
			DatastoreName: datastore,
			TransactionId: id,
		})
		callCancel()
		if err == nil {
			return nil
		}
		if !isTransientTxErr(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("confirm not accepted within %s: %w", txConfirmBudget, err)
		case <-time.After(backoff):
		}
		backoff = min(2*backoff, txConfirmBackoffMax)
	}
}

// cancelOwn cancels a transaction we opened, on a detached bounded context.
// The id is removed from pending when the data-server confirms it is no
// longer open; otherwise it stays pending for the next transaction.
func cancelOwn(ctx context.Context, gate *TxGate, cl txClient, datastore, id string) {
	log := log.FromContext(ctx).With("datastore", datastore, "transactionID", id)

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), txRPCTimeout)
	defer cancel()

	_, err := cl.TransactionCancel(ctx, &sdcpb.TransactionCancelRequest{
		DatastoreName: datastore,
		TransactionId: id,
	})
	if err == nil || isTxNotOpenErr(err) {
		delete(gate.pending, id)
		return
	}
	log.Warn("transaction cancel failed, retrying before next transaction", "err", err)
}

// cancelPending closes the transactions left open by earlier attempts. It
// returns a recoverable error when one of them is still open, since opening
// a new transaction would fail with "transaction ongoing" anyway.
func cancelPending(ctx context.Context, gate *TxGate, cl txClient, datastore string) error {
	log := log.FromContext(ctx).With("datastore", datastore)

	for _, id := range gate.pendingIDs() {
		if gate.now().Sub(gate.pending[id]) > txPendingMaxAge {
			// the data-server rollback timer has released it by now
			log.Warn("dropping stale pending transaction", "transactionID", id)
			delete(gate.pending, id)
			continue
		}
		cancelOwn(ctx, gate, cl, datastore, id)
	}
	if ids := gate.pendingIDs(); len(ids) > 0 {
		return NewTransactionError(
			fmt.Errorf("previous transaction(s) %s still open on datastore %s", strings.Join(ids, ","), datastore), true)
	}
	return nil
}

func hasIntentErrors(rsp *sdcpb.TransactionSetResponse) bool {
	for _, intent := range rsp.GetIntents() {
		if len(intent.GetErrors()) > 0 {
			return true
		}
	}
	return false
}

// data-server error messages that are not (yet) mapped to a specific gRPC
// code. Matched case-insensitively.
const (
	dsMsgTransactionOngoing = "transaction ongoing"    // ErrTransactionOngoing
	dsMsgDatastoreLocked    = "datastore is locked"    // ErrDatastoreLocked
	dsMsgContextDone        = "context is closed"      // ErrContextDone
	dsMsgNoOngoing          = "no ongoing transaction" // Confirm/Cancel without open transaction
	dsMsgNoActive           = "no active transaction"  // TransactionManager.GetTransaction
	dsMsgUnknownDatastore   = "unknown datastore"      // datastore lookup
)

func errMsgContains(err error, subs ...string) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if st, ok := status.FromError(err); ok {
		msg = strings.ToLower(st.Message())
	}
	for _, s := range subs {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

func grpcCode(err error) codes.Code {
	var txErr *TransactionError
	if errors.As(err, &txErr) {
		err = txErr.Err
	}
	if st, ok := status.FromError(err); ok {
		return st.Code()
	}
	return codes.Unknown
}

// isTransientTxErr: the datastore is busy or unreachable for now.
func isTransientTxErr(err error) bool {
	if err == nil {
		return false
	}
	switch grpcCode(err) {
	case codes.Aborted, codes.ResourceExhausted, codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return true
	}
	return errMsgContains(err, dsMsgTransactionOngoing, dsMsgDatastoreLocked, dsMsgContextDone)
}

// isTxNotOpenErr: the data-server says the transaction (or the datastore)
// does not exist, so there is nothing left to cancel.
func isTxNotOpenErr(err error) bool {
	if grpcCode(err) == codes.NotFound {
		return true
	}
	if errMsgContains(err, dsMsgNoOngoing, dsMsgNoActive, dsMsgUnknownDatastore) {
		return true
	}
	// "transaction id <id> is invalid": another transaction holds the slot,
	// ours is not open.
	return errMsgContains(err, "transaction id ") && errMsgContains(err, " is invalid")
}

// setNotRegistered: TransactionSet was refused before our transaction was
// registered in the data-server.
func setNotRegistered(err error) bool {
	switch grpcCode(err) {
	case codes.InvalidArgument, codes.NotFound:
		return true
	}
	return errMsgContains(err, dsMsgTransactionOngoing, dsMsgDatastoreLocked)
}