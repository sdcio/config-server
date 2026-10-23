package targetmanager

import (
	"context"
	"encoding/json"
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

// parkedMarker returns the marker stored in the ConfigReady message, whatever
// the condition reason, so the attempt count survives a wake-up.
func parkedMarker(cfg *configv1alpha1.Config) (condv1alpha1.UnrecoverableMessage, bool) {
	marker := condv1alpha1.UnrecoverableMessage{}
	cond := cfg.GetCondition(configv1alpha1.ConditionTypeConfigReady)
	if err := json.Unmarshal([]byte(cond.Message), &marker); err != nil || marker.SpecHash == "" {
		return condv1alpha1.UnrecoverableMessage{}, false
	}
	return marker, true
}

// nextUnrecoverableMarker builds the marker for a Config that just failed
// unrecoverably. Attempts continue counting only while the spec is unchanged.
func nextUnrecoverableMarker(ctx context.Context, current *configv1alpha1.Config, msg string) condv1alpha1.UnrecoverableMessage {
	hash := current.SpecHash(ctx)
	attempts := int32(1)
	if prev, ok := parkedMarker(current); ok && prev.SpecHash == hash {
		attempts = prev.Attempts + 1
	}
	return condv1alpha1.UnrecoverableMessage{SpecHash: hash, Attempts: attempts, Message: msg}
}

// parkedWakeCondition returns a recoverable ConfigReady failure that keeps the
// marker (spec hash, attempts) so the Config is retried and the backoff continues.
func parkedWakeCondition(cfg *configv1alpha1.Config, reason string) condv1alpha1.Condition {
	marker, _ := parkedMarker(cfg)
	marker.Message = fmt.Sprintf("retry requested: %s; last error: %s", reason, marker.Message)
	raw, _ := json.Marshal(marker)
	return configv1alpha1.ConfigFailed(string(raw))
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
		marker, _ := parkedMarker(cfg)
		cond := cfg.GetCondition(configv1alpha1.ConditionTypeConfigReady)
		remaining := cond.LastTransitionTime.Add(parkedRetryDelay(marker.Attempts)).Sub(now)
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
