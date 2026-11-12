package targetmanager

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/prototext"

	"github.com/sdcio/data-server/pkg/api"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDataServer is an in-memory DataServer: it keeps the datastores it was
// asked to create and records the transaction calls.
type fakeDataServer struct {
	mu         sync.Mutex
	datastores map[string]api.DatastoreSpec
	calls      []string

	getErr, createErr, deleteErr error
	setResult                    *api.TransactionResult
	setErr, confirmErr           error
	setIntents                   []api.Intent
	setOpts                      api.SetOptions
}

func newFakeDataServer() *fakeDataServer {
	return &fakeDataServer{datastores: map[string]api.DatastoreSpec{}}
}

func (f *fakeDataServer) record(call string) { f.calls = append(f.calls, call) }

func (f *fakeDataServer) GetDataStore(_ context.Context, name string) (*api.DatastoreInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("get")
	if f.getErr != nil {
		return nil, f.getErr
	}
	spec, ok := f.datastores[name]
	if !ok {
		return nil, fmt.Errorf("%w %s", api.ErrUnknownDatastore, name)
	}
	return &api.DatastoreInfo{Name: name, Spec: spec}, nil
}

func (f *fakeDataServer) CreateDataStore(_ context.Context, spec api.DatastoreSpec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("create")
	if f.createErr != nil {
		return f.createErr
	}
	f.datastores[spec.Name] = spec
	return nil
}

func (f *fakeDataServer) DeleteDataStore(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("delete")
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if _, ok := f.datastores[name]; !ok {
		return fmt.Errorf("%w %s", api.ErrUnknownDatastore, name)
	}
	delete(f.datastores, name)
	return nil
}

func (f *fakeDataServer) TransactionSet(_ context.Context, _ api.TxRef, opts api.SetOptions, intents []api.Intent) (*api.TransactionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("set")
	f.setOpts, f.setIntents = opts, intents
	return f.setResult, f.setErr
}

func (f *fakeDataServer) TransactionConfirm(context.Context, api.TxRef, api.Origin) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("confirm")
	return f.confirmErr
}

func (f *fakeDataServer) TransactionCancel(context.Context, api.TxRef, api.Origin) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("cancel")
	return nil
}

func (f *fakeDataServer) WatchDeviations(context.Context, string) (<-chan api.DeviationEvent, error) {
	return nil, errors.New("not implemented")
}

func runtimeWithDS(ds DataServer) *TargetRuntime {
	return NewTargetRuntime(testTargetKey(), ds, nil, nil, nil)
}

func specNamed(name string) *api.DatastoreSpec {
	spec := gnmiReq("10.0.0.1", 57400)
	spec.Name = name
	return spec
}

func TestEnsureDatastore(t *testing.T) {
	ctx := context.Background()
	name := testTargetKey().String()

	t.Run("creates an unknown datastore", func(t *testing.T) {
		ds := newFakeDataServer()
		rt := runtimeWithDS(ds)
		rt.SetDesired(specNamed(name), nil, "h1")

		require.NoError(t, rt.ensureDatastore(ctx, rt.getDesired()))
		assert.Equal(t, []string{"get", "create"}, ds.calls)
		assert.Equal(t, "h1", rt.getRunningHash())
		assert.True(t, rt.Status().DSStoreReady)
		assert.False(t, rt.Status().Recovered, "a fresh datastore holds no intents")
	})

	t.Run("keeps a matching datastore", func(t *testing.T) {
		ds := newFakeDataServer()
		ds.datastores[name] = *specNamed(name)
		rt := runtimeWithDS(ds)
		rt.SetDesired(specNamed(name), nil, "h1")
		rt.setRunningHash("h1")

		require.NoError(t, rt.ensureDatastore(ctx, rt.getDesired()))
		assert.Equal(t, []string{"get"}, ds.calls)
		assert.True(t, rt.Status().DSStoreReady)
	})

	t.Run("recreates on hash change", func(t *testing.T) {
		ds := newFakeDataServer()
		ds.datastores[name] = *specNamed(name)
		rt := runtimeWithDS(ds)
		rt.SetDesired(specNamed(name), nil, "h2")
		rt.setRunningHash("h1")

		require.NoError(t, rt.ensureDatastore(ctx, rt.getDesired()))
		assert.Equal(t, []string{"get", "delete", "create"}, ds.calls)
		assert.Equal(t, "h2", rt.getRunningHash())
	})

	t.Run("other lookup errors are not a create", func(t *testing.T) {
		ds := newFakeDataServer()
		ds.getErr = errors.New("boom")
		rt := runtimeWithDS(ds)
		rt.SetDesired(specNamed(name), nil, "h1")

		assert.Error(t, rt.ensureDatastore(ctx, rt.getDesired()))
		assert.Equal(t, []string{"get"}, ds.calls)
		assert.False(t, rt.Status().DSStoreReady)
	})

	t.Run("create failure is reported", func(t *testing.T) {
		ds := newFakeDataServer()
		ds.createErr = errors.New("invalid")
		rt := runtimeWithDS(ds)
		rt.SetDesired(specNamed(name), nil, "h1")

		assert.ErrorContains(t, rt.ensureDatastore(ctx, rt.getDesired()), "create datastore")
	})
}

func TestEnsureDatastoreDeleted(t *testing.T) {
	ctx := context.Background()
	name := testTargetKey().String()

	t.Run("deletes the datastore", func(t *testing.T) {
		ds := newFakeDataServer()
		ds.datastores[name] = *specNamed(name)
		rt := runtimeWithDS(ds)
		rt.setRunningHash("h1")

		rt.ensureDatastoreDeleted(ctx)
		assert.Empty(t, ds.datastores)
		assert.Empty(t, rt.getRunningHash())
	})

	t.Run("unknown datastore counts as deleted", func(t *testing.T) {
		rt := runtimeWithDS(newFakeDataServer())
		rt.ensureDatastoreDeleted(ctx)
		assert.NotEqual(t, PhaseDegraded, rt.Status().Phase)
	})

	t.Run("failure degrades the runtime", func(t *testing.T) {
		ds := newFakeDataServer()
		ds.deleteErr = errors.New("boom")
		rt := runtimeWithDS(ds)

		rt.ensureDatastoreDeleted(ctx)
		assert.Equal(t, PhaseDegraded, rt.Status().Phase)
	})
}

func TestTransactorTransactionSet(t *testing.T) {
	ctx := context.Background()
	ref := api.TxRef{Datastore: "default/dev1", ID: "tx1"}

	t.Run("confirms a successful set", func(t *testing.T) {
		ds := newFakeDataServer()
		ds.setResult = &api.TransactionResult{Intents: map[string]api.IntentValidation{"a": {Warnings: []string{"w"}}}}
		msg, err := (&Transactor{}).TransactionSet(ctx, &DatastoreHandle{Transactions: ds}, ref, api.SetOptions{}, nil)

		require.NoError(t, err)
		assert.Contains(t, msg, `intent "a" warning: "w"`)
		assert.Equal(t, []string{"set", "confirm"}, ds.calls)
	})

	t.Run("dry run is not confirmed", func(t *testing.T) {
		ds := newFakeDataServer()
		_, err := (&Transactor{}).TransactionSet(ctx, &DatastoreHandle{Transactions: ds}, ref, api.SetOptions{DryRun: true}, nil)

		require.NoError(t, err)
		assert.Equal(t, []string{"set"}, ds.calls)
	})

	t.Run("intent errors fail without confirm", func(t *testing.T) {
		ds := newFakeDataServer()
		ds.setResult = &api.TransactionResult{Intents: map[string]api.IntentValidation{"a": {Errors: []string{"bad"}}}}
		_, err := (&Transactor{}).TransactionSet(ctx, &DatastoreHandle{Transactions: ds}, ref, api.SetOptions{}, nil)

		var txErr *TransactionError
		require.ErrorAs(t, err, &txErr)
		assert.False(t, txErr.Recoverable)
		assert.Equal(t, []string{"set"}, ds.calls)
	})
}

func TestProcessTransactionResultRecoverable(t *testing.T) {
	cases := map[string]struct {
		err  error
		want bool
	}{
		"locked":        {err: fmt.Errorf("wrap: %w", api.ErrDatastoreLocked), want: true},
		"not connected": {err: api.ErrNotConnected, want: true},
		"deadline":      {err: context.DeadlineExceeded, want: true},
		"untyped":       {err: errors.New("boom"), want: false},
		"invalid":       {err: api.ErrInvalidArgument, want: false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := processTransactionResult(context.Background(), nil, tc.err)
			var txErr *TransactionError
			require.ErrorAs(t, err, &txErr)
			assert.Equal(t, tc.want, txErr.Recoverable)
			assert.Equal(t, tc.want, analyzeIntentResponse(tc.err, nil).Recoverable)
		})
	}

	t.Run("intent error is never recoverable", func(t *testing.T) {
		rsp := &api.TransactionResult{Intents: map[string]api.IntentValidation{"a": {Errors: []string{"bad"}}}}
		assert.False(t, analyzeIntentResponse(api.ErrDatastoreLocked, rsp).Recoverable)
	})
}

func mustPath(t *testing.T, s string) *sdcpb.Path {
	t.Helper()
	p, err := sdcpb.ParsePath(s)
	require.NoError(t, err)
	return p
}

func intVal(v int64) *sdcpb.TypedValue {
	return &sdcpb.TypedValue{Value: &sdcpb.TypedValue_IntVal{IntVal: v}}
}

func parseText(t *testing.T, s string) *sdcpb.TypedValue {
	t.Helper()
	tv := &sdcpb.TypedValue{}
	require.NoError(t, prototext.Unmarshal([]byte(s), tv))
	return tv
}

func TestConvertDeviationEvents(t *testing.T) {
	got := convertDeviationEvents([]api.DeviationEvent{
		{Reason: api.DeviationReasonNotApplied, Path: mustPath(t, "/a"), Expected: intVal(1), Current: intVal(2)},
		{Reason: api.DeviationReasonOverruled, Path: mustPath(t, "/b"), Expected: intVal(3)},
	})
	require.Len(t, got, 2)
	assert.Equal(t, "/a", got[0].Path)
	assert.Equal(t, "NOT_APPLIED", got[0].Reason)
	assert.Equal(t, intVal(1).String(), parseText(t, *got[0].DesiredValue).String())
	assert.Equal(t, intVal(2).String(), parseText(t, *got[0].ActualValue).String())
	assert.Equal(t, "OVERRULED", got[1].Reason)
	assert.Nil(t, got[1].ActualValue, "absent value stays nil")
}

// A closed watch ends consume; events outside a run are ignored.
func TestDeviationWatcherConsumeEnds(t *testing.T) {
	w := NewDeviationWatcher(testTargetKey(), nil, newFakeDataServer())
	ch := make(chan api.DeviationEvent, 2)
	ch <- api.DeviationEvent{Kind: api.DeviationUpdate} // update without start
	ch <- api.DeviationEvent{Kind: api.DeviationEnd}    // end without start
	close(ch)

	done := make(chan struct{})
	go func() { w.consume(context.Background(), ch); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("consume did not return after the channel closed")
	}
}
