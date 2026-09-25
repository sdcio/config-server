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

package config

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/types"
)

func TestResolveClearDeviationConfig(t *testing.T) {
	cfg := &Config{}
	cfg.Name = "intent1-srl-srl1"

	configs := map[string]*Config{cfg.Name: cfg}
	targetKey := types.NamespacedName{Namespace: "default", Name: "srl1"}

	cases := map[string]struct {
		input        string
		wantCfg      *Config
		wantErrMatch string
	}{
		"exact match by config name":     {"intent1-srl-srl1", cfg, ""},
		"match via deviation prefix":     {"config-intent1-srl-srl1", cfg, ""},
		"unknown name":                   {"missing", nil, "not found"},
		"unknown after prefix strip":     {"config-missing", nil, "not found"},
		"target-typed deviation rejects": {"target-srl1", nil, "target-scoped"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := resolveClearDeviationConfig(tc.input, configs, targetKey)
			if tc.wantErrMatch != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErrMatch) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErrMatch, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantCfg {
				t.Fatalf("got %v, want %v", got, tc.wantCfg)
			}
		})
	}
}

func TestIsRecoverableClearDeviationTxError(t *testing.T) {
	cases := map[string]struct {
		err  error
		want bool
	}{
		"nil":                 {nil, false},
		"aborted":             {status.Error(codes.Aborted, "transaction ongoing"), true},
		"resource exhausted":  {status.Error(codes.ResourceExhausted, "backpressure"), true},
		"unavailable":         {status.Error(codes.Unavailable, "not connected"), true},
		"deadline exceeded":   {status.Error(codes.DeadlineExceeded, "timeout"), true},
		"failed precondition": {status.Error(codes.FailedPrecondition, "invalid"), false},
		"plain error":         {errors.New("boom"), false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := isRecoverableClearDeviationTxError(tc.err); got != tc.want {
				t.Errorf("isRecoverableClearDeviationTxError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestRetryClearDeviationTx(t *testing.T) {
	t.Run("succeeds without retry", func(t *testing.T) {
		calls := 0
		rsp, err := retryClearDeviationTx(context.Background(), 4, time.Millisecond, func() (*sdcpb.TransactionSetResponse, error) {
			calls++
			return &sdcpb.TransactionSetResponse{}, nil
		})
		if err != nil || rsp == nil {
			t.Fatalf("got rsp=%v err=%v, want success", rsp, err)
		}
		if calls != 1 {
			t.Errorf("calls = %d, want 1 (no retry needed)", calls)
		}
	})

	t.Run("retries recoverable error then succeeds", func(t *testing.T) {
		calls := 0
		rsp, err := retryClearDeviationTx(context.Background(), 4, time.Millisecond, func() (*sdcpb.TransactionSetResponse, error) {
			calls++
			if calls < 3 {
				return nil, status.Error(codes.Aborted, "transaction ongoing")
			}
			return &sdcpb.TransactionSetResponse{}, nil
		})
		if err != nil || rsp == nil {
			t.Fatalf("got rsp=%v err=%v, want eventual success", rsp, err)
		}
		if calls != 3 {
			t.Errorf("calls = %d, want 3", calls)
		}
	})

	t.Run("stops immediately on non-recoverable error", func(t *testing.T) {
		calls := 0
		_, err := retryClearDeviationTx(context.Background(), 4, time.Millisecond, func() (*sdcpb.TransactionSetResponse, error) {
			calls++
			return nil, status.Error(codes.FailedPrecondition, "invalid")
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if calls != 1 {
			t.Errorf("calls = %d, want 1 (should not retry non-recoverable error)", calls)
		}
	})

	t.Run("gives up after maxAttempts on persistent recoverable error", func(t *testing.T) {
		calls := 0
		_, err := retryClearDeviationTx(context.Background(), 3, time.Millisecond, func() (*sdcpb.TransactionSetResponse, error) {
			calls++
			return nil, status.Error(codes.Aborted, "transaction ongoing")
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if calls != 3 {
			t.Errorf("calls = %d, want 3 (maxAttempts)", calls)
		}
	})

	t.Run("aborts retries when context is cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		_, err := retryClearDeviationTx(ctx, 5, 50*time.Millisecond, func() (*sdcpb.TransactionSetResponse, error) {
			calls++
			if calls == 1 {
				cancel()
			}
			return nil, status.Error(codes.Aborted, "transaction ongoing")
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if calls != 1 {
			t.Errorf("calls = %d, want 1 (should stop after context cancellation)", calls)
		}
	})
}
