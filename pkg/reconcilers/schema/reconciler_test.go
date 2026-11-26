/*
Copyright 2024 Nokia.

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

package schema

import (
	"context"
	"errors"
	"fmt"
	"testing"

	sdcerrors "github.com/sdcio/config-server/pkg/errors"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	ctrl "sigs.k8s.io/controller-runtime"
)

func TestRequeueFor(t *testing.T) {
	dialTimeout := fmt.Errorf("gRPC connect timeout to %q; last state: %s: %w",
		"schema-server:56000", "TRANSIENT_FAILURE", context.DeadlineExceeded)

	cases := map[string]struct {
		err  error
		want ctrl.Result
	}{
		"in-progress state (nil err)": {nil, ctrl.Result{RequeueAfter: inProgressRequeue}},
		"dial timeout":                {dialTimeout, ctrl.Result{RequeueAfter: inProgressRequeue}},
		"grpc unavailable":            {grpcstatus.Error(codes.Unavailable, "down"), ctrl.Result{RequeueAfter: inProgressRequeue}},
		"grpc deadline":               {grpcstatus.Error(codes.DeadlineExceeded, "slow"), ctrl.Result{RequeueAfter: inProgressRequeue}},
		"grpc invalid argument":       {grpcstatus.Error(codes.InvalidArgument, "bad yang"), ctrl.Result{RequeueAfter: errorRequeue}},
		"plain error":                 {errors.New("boom"), ctrl.Result{RequeueAfter: errorRequeue}},
		"unrecoverable": {
			&sdcerrors.UnrecoverableError{Message: "bad url"},
			ctrl.Result{},
		},
		"wrapped unrecoverable": {
			fmt.Errorf("load: %w", &sdcerrors.UnrecoverableError{Message: "bad url", WrappedError: context.DeadlineExceeded}),
			ctrl.Result{},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := requeueFor(tc.err); got != tc.want {
				t.Errorf("requeueFor(%v) = %+v, want %+v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsTransientGetSchemaDetailsUnknown(t *testing.T) {
	// schema-server answers an unknown schema with InvalidArgument: that must
	// read as "absent" (create / skip delete), not as "retry later".
	err := grpcstatus.Errorf(codes.InvalidArgument, "unknown schema %v", "x")
	if isTransient(err) {
		t.Fatalf("unknown-schema answer classified as transient")
	}
}