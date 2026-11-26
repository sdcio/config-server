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

	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
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

func TestSpecDrift(t *testing.T) {
	repo := func(url string, kind invv1alpha1.BranchTagKind, ref string) *invv1alpha1.SchemaSpecRepository {
		return &invv1alpha1.SchemaSpecRepository{Repository: invv1alpha1.Repository{RepoURL: url, Kind: kind, Ref: ref}}
	}
	loaded := func(url string, kind invv1alpha1.BranchTagKind, ref string) invv1alpha1.SchemaRepositoryStatus {
		return invv1alpha1.SchemaRepositoryStatus{RepoURL: url, Kind: kind, Ref: ref, Reference: "c0ffee"}
	}
	const u1, u2 = "https://example.com/a.git", "https://example.com/b.git"
	branch, tag := invv1alpha1.BranchTagKindBranch, invv1alpha1.BranchTagKindTag

	cases := map[string]struct {
		spec   []*invv1alpha1.SchemaSpecRepository
		loaded []invv1alpha1.SchemaRepositoryStatus
		drift  bool
	}{
		"unchanged": {
			[]*invv1alpha1.SchemaSpecRepository{repo(u1, branch, "main"), repo(u2, tag, "v1")},
			[]invv1alpha1.SchemaRepositoryStatus{loaded(u1, branch, "main"), loaded(u2, tag, "v1")},
			false,
		},
		"branch changed (issue 449)": {
			[]*invv1alpha1.SchemaSpecRepository{repo(u1, branch, "non-existing-branch")},
			[]invv1alpha1.SchemaRepositoryStatus{loaded(u1, branch, "main")},
			true,
		},
		"kind changed": {
			[]*invv1alpha1.SchemaSpecRepository{repo(u1, tag, "main")},
			[]invv1alpha1.SchemaRepositoryStatus{loaded(u1, branch, "main")},
			true,
		},
		"url changed": {
			[]*invv1alpha1.SchemaSpecRepository{repo(u2, branch, "main")},
			[]invv1alpha1.SchemaRepositoryStatus{loaded(u1, branch, "main")},
			true,
		},
		"repository added": {
			[]*invv1alpha1.SchemaSpecRepository{repo(u1, branch, "main"), repo(u2, tag, "v1")},
			[]invv1alpha1.SchemaRepositoryStatus{loaded(u1, branch, "main")},
			true,
		},
		"nothing recorded": {
			[]*invv1alpha1.SchemaSpecRepository{repo(u1, branch, "main")},
			nil,
			true,
		},
		"status from before kind/ref were recorded": {
			[]*invv1alpha1.SchemaSpecRepository{repo(u1, branch, "main")},
			[]invv1alpha1.SchemaRepositoryStatus{{RepoURL: u1, Reference: "c0ffee"}},
			true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := specDrift(tc.spec, tc.loaded)
			if (got != "") != tc.drift {
				t.Errorf("specDrift() = %q, want drift=%v", got, tc.drift)
			}
		})
	}
}

func TestPollResult(t *testing.T) {
	schema := func(kinds ...invv1alpha1.BranchTagKind) *invv1alpha1.Schema {
		s := &invv1alpha1.Schema{}
		for _, k := range kinds {
			s.Spec.Repositories = append(s.Spec.Repositories,
				&invv1alpha1.SchemaSpecRepository{Repository: invv1alpha1.Repository{Kind: k}})
		}
		return s
	}
	if got := pollResult(schema(invv1alpha1.BranchTagKindTag, invv1alpha1.BranchTagKindHash)); got != (ctrl.Result{}) {
		t.Errorf("tags/hashes only: got %+v, want no requeue", got)
	}
	if got := pollResult(schema(invv1alpha1.BranchTagKindTag, invv1alpha1.BranchTagKindBranch)); got.RequeueAfter != upstreamPollInterval {
		t.Errorf("with a branch: got %+v, want RequeueAfter %s", got, upstreamPollInterval)
	}
}
