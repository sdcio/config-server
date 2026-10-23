package targetmanager

import (
	"context"
	"fmt"
	"time"

	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
)

const (
	// parkedRetryBase is the delay before the first timed retry of a parked Config.
	parkedRetryBase = time.Minute
	// parkedRetryMax caps the timed retry backoff.
	parkedRetryMax = 30 * time.Minute
)

// parkedRetryDelay is the wait after the given number of failed attempts before
// a parked Config is retried by the timer: doubles per attempt up to parkedRetryMax.
func parkedRetryDelay(attempts int32) time.Duration {
	delay := parkedRetryBase
	for i := int32(1); i < attempts && delay < parkedRetryMax; i++ {
		delay *= 2
	}
	return min(delay, parkedRetryMax)
}

// nextParked builds status.parked for a Config that just failed unrecoverably.
// Attempts continue counting only while the spec is unchanged.
func nextParked(ctx context.Context, current *configv1alpha1.Config) *configv1alpha1.ConfigParked {
	hash := current.SpecHash(ctx)
	attempts := int32(1)
	if prev := current.Status.Parked; prev != nil && prev.SpecHash == hash {
		attempts = prev.Attempts + 1
	}
	return &configv1alpha1.ConfigParked{SpecHash: hash, Attempts: attempts}
}

// parkedWakeCondition returns the recoverable ConfigReady failure that wakes a
// parked Config. status.parked stays untouched so the backoff continues.
func parkedWakeCondition(cfg *configv1alpha1.Config, reason string) condv1alpha1.Condition {
	last := cfg.GetCondition(configv1alpha1.ConditionTypeConfigReady).Message
	return configv1alpha1.ConfigFailed(fmt.Sprintf("retry requested: %s; last error: %s", reason, last))
}

// selectParkedToWake returns the parked Configs to retry now and the time until
// the next parked Config is due (0 when none is waiting). With all set every
// parked Config is returned regardless of its backoff.
func selectParkedToWake(ctx context.Context, configs []configv1alpha1.Config, now time.Time, all bool) ([]*configv1alpha1.Config, time.Duration) {
	var wake []*configv1alpha1.Config
	var next time.Duration
	for i := range configs {
		cfg := &configs[i]
		if cfg.GetDeletionTimestamp() != nil || cfg.IsRecoverable(ctx) {
			continue
		}
		cond := cfg.GetCondition(configv1alpha1.ConditionTypeConfigReady)
		remaining := cond.LastTransitionTime.Add(parkedRetryDelay(cfg.Status.Parked.Attempts)).Sub(now)
		if all || remaining <= 0 {
			wake = append(wake, cfg)
			continue
		}
		if next == 0 || remaining < next {
			next = remaining
		}
	}
	return wake, next
}
