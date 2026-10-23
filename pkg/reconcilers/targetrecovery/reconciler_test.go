package targetconfigserver

import (
	"errors"
	"strings"
	"testing"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
)

func TestFailedRecoveryResult_ReturnsErrorWithoutFixedRequeue(t *testing.T) {
	recoveryErr := errors.New("recover failed")

	res, err := failedRecoveryResult(recoveryErr, nil)

	if err == nil {
		t.Fatal("expected the recovery error to be returned so backoff applies")
	}
	if !errors.Is(err, recoveryErr) {
		t.Fatalf("expected error to wrap the recovery error, got %v", err)
	}
	if res != (ctrl.Result{}) {
		t.Fatalf("expected empty result (no fixed requeue), got %+v", res)
	}
}

func TestFailedRecoveryResult_KeepsStatusError(t *testing.T) {
	statusErr := errors.New("status update failed")

	_, err := failedRecoveryResult(errors.New("recover failed"), statusErr)

	if !errors.Is(err, statusErr) {
		t.Fatalf("expected error to include the status error, got %v", err)
	}
}

func TestRecoveryFailureMessage_NotTruncated(t *testing.T) {
	long := strings.Repeat("x", 500)

	msg := recoveryFailureMessage("setIntent failed", errors.New(long))

	if !strings.Contains(msg, long) {
		t.Fatalf("message was truncated, len=%d", len(msg))
	}
}

func TestRecoveryRateLimiter_BackoffFrom1sCappedAt60s(t *testing.T) {
	rl := newRecoveryRateLimiter()
	req := ctrl.Request{}

	want := []time.Duration{1, 2, 4, 8, 16, 32, 60, 60}
	for i, w := range want {
		if got := rl.When(req); got != w*time.Second {
			t.Fatalf("failure %d: got %s, want %s", i+1, got, w*time.Second)
		}
	}
}
