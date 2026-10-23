package targetmanager

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	internalconfig "github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newParkedConfig(t *testing.T, name string, attempts int32, parkedAt time.Time) *configv1alpha1.Config {
	t.Helper()
	cfg := &configv1alpha1.Config{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec:       configv1alpha1.ConfigSpec{Priority: 10},
	}
	raw, err := json.Marshal(condv1alpha1.UnrecoverableMessage{
		SpecHash: cfg.SpecHash(context.Background()),
		Attempts: attempts,
		Message:  "validation failed",
	})
	require.NoError(t, err)
	cond := configv1alpha1.ConfigFailedUnrecoverable(string(raw))
	cond.LastTransitionTime = metav1.NewTime(parkedAt)
	cfg.SetConditions(cond)
	return cfg
}

func newReadyConfig(name string) *configv1alpha1.Config {
	cfg := &configv1alpha1.Config{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec:       configv1alpha1.ConfigSpec{Priority: 10},
	}
	cfg.SetConditions(configv1alpha1.ConfigReady("applied"))
	return cfg
}

func TestParkedRetryDelay_BacksOffAndCaps(t *testing.T) {
	cases := map[int32]time.Duration{
		0:  time.Minute,
		1:  time.Minute,
		2:  2 * time.Minute,
		3:  4 * time.Minute,
		5:  16 * time.Minute,
		6:  30 * time.Minute,
		40: 30 * time.Minute,
	}
	for attempts, want := range cases {
		assert.Equal(t, want, parkedRetryDelay(attempts), "attempts=%d", attempts)
	}
}

func TestSelectParkedToWake_TimerHonorsBackoff(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	notDue := newParkedConfig(t, "not-due", 1, now.Add(-30*time.Second))
	due := newParkedConfig(t, "due", 1, now.Add(-2*time.Minute))
	backedOff := newParkedConfig(t, "backed-off", 3, now.Add(-2*time.Minute)) // needs 4m
	ready := newReadyConfig("ready")

	wake, next := selectParkedToWake(ctx, []configv1alpha1.Config{*notDue, *due, *backedOff, *ready}, now, false)

	require.Len(t, wake, 1)
	assert.Equal(t, "due", wake[0].Name)
	assert.Equal(t, 30*time.Second, next, "next wake is the earliest remaining backoff (not-due: 1m - 30s)")
}

func TestSelectParkedToWake_AllWakesEveryParkedConfig(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	a := newParkedConfig(t, "a", 1, now)
	b := newParkedConfig(t, "b", 5, now)
	ready := newReadyConfig("ready")

	wake, _ := selectParkedToWake(ctx, []configv1alpha1.Config{*a, *b, *ready}, now, true)

	assert.Len(t, wake, 2)
}

func TestSelectParkedToWake_NothingParked(t *testing.T) {
	wake, next := selectParkedToWake(context.Background(), []configv1alpha1.Config{*newReadyConfig("r")}, time.Now(), false)
	assert.Empty(t, wake)
	assert.Zero(t, next)
}

func TestNextUnrecoverableMarker_CountsAttemptsForSameSpec(t *testing.T) {
	ctx := context.Background()
	prev := newParkedConfig(t, "c", 2, time.Now())

	got := nextUnrecoverableMarker(ctx, prev, "boom")

	assert.Equal(t, int32(3), got.Attempts)
	assert.Equal(t, prev.SpecHash(ctx), got.SpecHash)
	assert.Equal(t, "boom", got.Message)
}

func TestNextUnrecoverableMarker_ResetsOnSpecChange(t *testing.T) {
	ctx := context.Background()
	prev := newParkedConfig(t, "c", 4, time.Now())
	prev.Spec.Priority = 99

	got := nextUnrecoverableMarker(ctx, prev, "boom")

	assert.Equal(t, int32(1), got.Attempts)
}

func TestNextUnrecoverableMarker_FirstFailure(t *testing.T) {
	got := nextUnrecoverableMarker(context.Background(), newReadyConfig("c"), "boom")
	assert.Equal(t, int32(1), got.Attempts)
}

func TestWakeCondition_KeepsAttemptsAndIsRecoverable(t *testing.T) {
	ctx := context.Background()
	parked := newParkedConfig(t, "c", 3, time.Now())

	cond := parkedWakeCondition(parked, "recovery re-ran")
	parked.SetConditions(cond)

	assert.NotEqual(t, string(condv1alpha1.ConditionReasonUnrecoverable), cond.Reason)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)

	internal := toInternalConfig(t, parked)
	assert.True(t, internal.IsRecoverable(ctx), "woken config must be retried")

	// a failure after the wake continues the backoff instead of restarting it
	got := nextUnrecoverableMarker(ctx, parked, "boom")
	assert.Equal(t, int32(4), got.Attempts)
}

func transactConfigs(t *testing.T, cfgs ...*configv1alpha1.Config) (map[string]*internalconfig.Config, map[string]*internalconfig.Config) {
	t.Helper()
	list := &internalconfig.ConfigList{}
	for _, c := range cfgs {
		list.Items = append(list.Items, *toInternalConfig(t, c))
	}
	return getConfigsToTransact(context.Background(), list)
}

func TestGetConfigsToTransact_ParkedConfigAloneIsNotRetried(t *testing.T) {
	parked := newParkedConfig(t, "parked", 1, time.Now())
	sibling := newReadyConfig("sibling")
	sibling.Status.AppliedConfig = &sibling.Spec

	update, del := transactConfigs(t, parked, sibling)

	assert.Empty(t, update)
	assert.Empty(t, del)
}

func TestGetConfigsToTransact_SiblingUpdateRetriesParkedConfig(t *testing.T) {
	parked := newParkedConfig(t, "parked", 1, time.Now())
	sibling := newReadyConfig("sibling") // no AppliedConfig -> pending update

	update, _ := transactConfigs(t, parked, sibling)

	assert.Len(t, update, 2)
}

func TestGetConfigsToTransact_SiblingDeleteRetriesParkedConfig(t *testing.T) {
	parked := newParkedConfig(t, "parked", 1, time.Now())
	sibling := newReadyConfig("sibling")
	now := metav1.Now()
	sibling.DeletionTimestamp = &now

	update, del := transactConfigs(t, parked, sibling)

	assert.Len(t, del, 1)
	assert.Len(t, update, 1, "parked config is retried alongside the delete")
}

func TestGetConfigsToTransact_OwnSpecChangeRetriesParkedConfig(t *testing.T) {
	parked := newParkedConfig(t, "parked", 1, time.Now())
	parked.Spec.Priority = 99

	update, _ := transactConfigs(t, parked)

	assert.Len(t, update, 1)
}
