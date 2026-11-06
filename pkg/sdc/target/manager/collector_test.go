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
	"fmt"
	"testing"
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/openconfig/gnmi/proto/gnmi"
	gapi "github.com/openconfig/gnmic/pkg/api"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
)

func testTargetKey() storebackend.Key {
	return storebackend.KeyFromNSN(types.NamespacedName{Namespace: "default", Name: "dev1"})
}

func TestCollectorHasPathsChanged(t *testing.T) {
	a0 := Path{Path: "/a", Interval: 0}
	a30 := Path{Path: "/a", Interval: 30}
	b0 := Path{Path: "/b", Interval: 0}

	cases := map[string]struct {
		old  map[invv1alpha1.Encoding][]Path
		new  map[invv1alpha1.Encoding][]Path
		want bool
	}{
		"nil -> empty": {
			old: nil, new: map[invv1alpha1.Encoding][]Path{}, want: false,
		},
		"nil -> paths": {
			old: nil, new: map[invv1alpha1.Encoding][]Path{invv1alpha1.Encoding_ASCII: {a0}}, want: true,
		},
		"identical": {
			old:  map[invv1alpha1.Encoding][]Path{invv1alpha1.Encoding_ASCII: {a0, b0}},
			new:  map[invv1alpha1.Encoding][]Path{invv1alpha1.Encoding_ASCII: {a0, b0}},
			want: false,
		},
		"interval changed": {
			old:  map[invv1alpha1.Encoding][]Path{invv1alpha1.Encoding_ASCII: {a0}},
			new:  map[invv1alpha1.Encoding][]Path{invv1alpha1.Encoding_ASCII: {a30}},
			want: true,
		},
		"path added": {
			old:  map[invv1alpha1.Encoding][]Path{invv1alpha1.Encoding_ASCII: {a0}},
			new:  map[invv1alpha1.Encoding][]Path{invv1alpha1.Encoding_ASCII: {a0, b0}},
			want: true,
		},
		"path removed": {
			old:  map[invv1alpha1.Encoding][]Path{invv1alpha1.Encoding_ASCII: {a0, b0}},
			new:  map[invv1alpha1.Encoding][]Path{invv1alpha1.Encoding_ASCII: {a0}},
			want: true,
		},
		"encoding changed": {
			old:  map[invv1alpha1.Encoding][]Path{invv1alpha1.Encoding_ASCII: {a0}},
			new:  map[invv1alpha1.Encoding][]Path{invv1alpha1.Encoding_PROTO: {a0}},
			want: true,
		},
		"all paths gone": {
			old:  map[invv1alpha1.Encoding][]Path{invv1alpha1.Encoding_ASCII: {a0}},
			new:  map[invv1alpha1.Encoding][]Path{},
			want: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := NewCollector(testTargetKey(), NewSubscriptions())
			c.paths = tc.old
			assert.Equal(t, tc.want, c.hasPathsChanged(tc.new))
		})
	}
}

func TestBuildSubscribeRequests(t *testing.T) {
	paths := map[invv1alpha1.Encoding][]Path{
		invv1alpha1.Encoding_ASCII: {{Path: "/a", Interval: 0}},
		invv1alpha1.Encoding_PROTO: {{Path: "/b", Interval: 30}},
	}

	reqs, err := buildSubscribeRequests(paths)
	require.NoError(t, err)
	require.Len(t, reqs, 2)

	asciiName := fmt.Sprintf("configserver %s", invv1alpha1.Encoding_ASCII.String())
	protoName := fmt.Sprintf("configserver %s", invv1alpha1.Encoding_PROTO.String())

	t.Run("onChange", func(t *testing.T) {
		req, ok := reqs[asciiName]
		require.True(t, ok, "missing request %q", asciiName)
		sl := req.GetSubscribe()
		require.NotNil(t, sl)
		assert.Equal(t, gnmi.SubscriptionList_STREAM, sl.GetMode())
		assert.Equal(t, gnmi.Encoding_ASCII, sl.GetEncoding())
		require.Len(t, sl.GetSubscription(), 1)
		assert.Equal(t, gnmi.SubscriptionMode_ON_CHANGE, sl.GetSubscription()[0].GetMode())
	})

	t.Run("sample", func(t *testing.T) {
		req, ok := reqs[protoName]
		require.True(t, ok, "missing request %q", protoName)
		sl := req.GetSubscribe()
		require.NotNil(t, sl)
		assert.Equal(t, gnmi.Encoding_PROTO, sl.GetEncoding())
		require.Len(t, sl.GetSubscription(), 1)
		sub := sl.GetSubscription()[0]
		assert.Equal(t, gnmi.SubscriptionMode_SAMPLE, sub.GetMode())
		assert.Equal(t, uint64(30*time.Second), sub.GetSampleInterval())
	})
}

func TestBuildSubscribeRequestsEmpty(t *testing.T) {
	reqs, err := buildSubscribeRequests(nil)
	assert.NoError(t, err)
	assert.Empty(t, reqs)
}

func TestEncoding(t *testing.T) {
	cases := map[string]struct {
		in   string
		want int
	}{
		"upper":   {in: "ASCII", want: int(gnmi.Encoding_ASCII)},
		"lower":   {in: "json_ietf", want: int(gnmi.Encoding_JSON_IETF)},
		"numeric": {in: "2", want: 2},
		"unknown": {in: "bogus", want: 0},
		"empty":   {in: "", want: 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, encoding(tc.in))
		})
	}
}

func TestCollectorStartGuards(t *testing.T) {
	cases := map[string]struct {
		req     *sdcpb.CreateDataStoreRequest
		address string
	}{
		"nil request": {req: nil, address: "10.0.0.1:57400"},
		"nil target":  {req: &sdcpb.CreateDataStoreRequest{}, address: "10.0.0.1:57400"},
		"no address": {
			req:     &sdcpb.CreateDataStoreRequest{Target: &sdcpb.Target{Type: "gnmi", Address: "10.0.0.1"}},
			address: "",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := NewCollector(testTargetKey(), NewSubscriptions())
			err := c.Start(context.Background(), tc.req, tc.address)
			assert.Error(t, err)
			assert.False(t, c.IsRunning())
			assert.Empty(t, c.Address())
		})
	}
}

func TestCollectorStopNeverStarted(t *testing.T) {
	c := NewCollector(testTargetKey(), NewSubscriptions())
	assert.NotPanics(t, func() {
		c.Stop(context.Background())
		c.Stop(context.Background()) // idempotent
	})
	assert.False(t, c.IsRunning())
	assert.Empty(t, c.Address())
}

// Notifications must coalesce and never block the caller.
func TestCollectorNotifyCoalesces(t *testing.T) {
	c := NewCollector(testTargetKey(), NewSubscriptions())

	done := make(chan struct{})
	go func() {
		for i := 0; i < 10; i++ {
			c.NotifySubscriptionChanged()
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("NotifySubscriptionChanged blocked")
	}
	assert.Equal(t, 1, len(c.subChan))
}

// update() on a stopped collector (no target) must be a no-op.
func TestCollectorUpdateWithoutTarget(t *testing.T) {
	subs := NewSubscriptions()
	require.NoError(t, subs.AddSubscription(
		newTestSub("sub1", invv1alpha1.Encoding_ASCII, onChange("sub1", invv1alpha1.AdminState_ENABLED, testPath))))

	c := NewCollector(testTargetKey(), subs)
	c.update(context.Background())

	assert.Nil(t, c.paths)
	assert.Nil(t, c.subscriptionCancel)
}

// A loop left over from a previous Start (cancelled ctx) must not take over
// the path snapshot of the current target.
func TestCollectorUpdateStaleContext(t *testing.T) {
	subs := NewSubscriptions()
	require.NoError(t, subs.AddSubscription(
		newTestSub("sub1", invv1alpha1.Encoding_ASCII, onChange("sub1", invv1alpha1.AdminState_ENABLED, testPath))))

	// NewTarget does not dial, so this needs no device.
	tg, err := gapi.NewTarget(gapi.Name("dev1"), gapi.Address("127.0.0.1:57400"), gapi.Insecure(true))
	require.NoError(t, err)

	c := NewCollector(testTargetKey(), subs)
	c.target = tg

	staleCtx, cancel := context.WithCancel(context.Background())
	cancel()
	c.update(staleCtx)

	assert.Nil(t, c.paths, "stale loop must not record a path snapshot")
	assert.Nil(t, c.subscriptionCancel, "stale loop must not start a subscription")
}
