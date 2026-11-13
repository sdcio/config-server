package rollout

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sdcio/data-server/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
)

type fakeTx struct {
	set, confirm, cancel []api.TxRef
	err                  error
	block                chan struct{}
}

func (f *fakeTx) TransactionSet(ctx context.Context, ref api.TxRef, _ api.SetOptions, _ []api.Intent) (*api.TransactionResult, error) {
	f.set = append(f.set, ref)
	return nil, f.wait(ctx)
}

func (f *fakeTx) TransactionConfirm(ctx context.Context, ref api.TxRef, _ api.Origin) error {
	f.confirm = append(f.confirm, ref)
	return f.wait(ctx)
}

func (f *fakeTx) TransactionCancel(ctx context.Context, ref api.TxRef, _ api.Origin) error {
	f.cancel = append(f.cancel, ref)
	return f.wait(ctx)
}

func (f *fakeTx) wait(ctx context.Context) error {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.err
}

func newTestTransactionManager(tx transactionClient, timeout time.Duration) *transactionManager {
	return &transactionManager{tx: tx, targetTimeout: timeout}
}

func TestTransactionCalls(t *testing.T) {
	ctx := context.Background()
	key := types.NamespacedName{Namespace: "default", Name: "dev1"}
	want := []api.TxRef{{Datastore: "default.dev1", ID: "tx1"}}

	t.Run("confirm", func(t *testing.T) {
		tx := &fakeTx{}
		require.NoError(t, newTestTransactionManager(tx, time.Second).confirm(ctx, key, "tx1"))
		assert.Equal(t, want, tx.confirm)
	})

	t.Run("cancel", func(t *testing.T) {
		tx := &fakeTx{}
		require.NoError(t, newTestTransactionManager(tx, time.Second).cancel(ctx, key, "tx1"))
		assert.Equal(t, want, tx.cancel)
	})

	t.Run("errors are returned", func(t *testing.T) {
		tx := &fakeTx{err: errors.New("locked")}
		assert.ErrorContains(t, newTestTransactionManager(tx, time.Second).confirm(ctx, key, "tx1"), "locked")
	})

	t.Run("target timeout bounds the call", func(t *testing.T) {
		tx := &fakeTx{block: make(chan struct{})}
		defer close(tx.block)
		err := newTestTransactionManager(tx, 20*time.Millisecond).confirm(ctx, key, "tx1")
		assert.ErrorContains(t, err, "timeout")
	})
}
