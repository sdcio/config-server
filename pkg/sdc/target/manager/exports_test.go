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
	"testing"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/data-server/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

func exportsTestRuntime() *TargetRuntime {
	key := storebackend.KeyFromNSN(types.NamespacedName{Namespace: "default", Name: "dev1"})
	return NewTargetRuntime(key, nil, nil, nil, nil)
}

// The exporter reads the collector while the run loop starts/stops it.
// Must never panic; run with -race to also catch the data race (PR #513 review).
func TestCacheConcurrentWithCollectorStop(t *testing.T) {
	rt := exportsTestRuntime()
	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			rt.collector.Store(NewCollector(rt.key, rt.subscriptions))
			rt.stopCollector(ctx)
		}
	}()

	for {
		select {
		case <-done:
			assert.Nil(t, rt.Cache(), "no collector after the last stop")
			return
		default:
			_ = rt.Cache()
		}
	}
}

func TestCacheReturnsCurrentCollectorCache(t *testing.T) {
	rt := exportsTestRuntime()
	assert.Nil(t, rt.Cache())

	c := NewCollector(rt.key, rt.subscriptions)
	rt.collector.Store(c)
	assert.Equal(t, c.cache, rt.Cache())

	assert.True(t, rt.stopCollector(context.Background()))
	assert.Nil(t, rt.Cache())
	assert.False(t, rt.stopCollector(context.Background()), "second stop is a no-op")
}

func TestPromLabels(t *testing.T) {
	cases := map[string]struct {
		desired *api.DatastoreSpec
		want    map[string]string
	}{
		"no desired": {
			desired: nil,
			want:    map[string]string{"target": "dev1", "namespace": "default"},
		},
		"schema without target": {
			desired: &api.DatastoreSpec{
				Schema: api.SchemaRef{Vendor: "srl.nokia.sdcio.dev", Version: "24.10.2"},
			},
			want: map[string]string{"target": "dev1", "namespace": "default",
				"vendor": "srl.nokia.sdcio.dev", "version": "24.10.2", "address": ""},
		},
		"schema and target": {
			desired: &api.DatastoreSpec{
				Schema: api.SchemaRef{Vendor: "srl.nokia.sdcio.dev", Version: "24.10.2"},
				Target: api.Target{Address: "172.21.0.3"},
			},
			want: map[string]string{"target": "dev1", "namespace": "default",
				"vendor": "srl.nokia.sdcio.dev", "version": "24.10.2", "address": "172.21.0.3"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rt := exportsTestRuntime()
			rt.desired = tc.desired

			var got map[string]string
			assert.NotPanics(t, func() {
				got = map[string]string{}
				for _, l := range rt.PromLabels() {
					got[l.Name] = l.Value
				}
			})
			assert.Equal(t, tc.want, got)
		})
	}
}

// Locks the contract hasPathsChanged relies on: GetPaths returns the paths
// of each encoding sorted, whatever the insertion order (PR #513 review).
func TestGetPathsSortedPerEncoding(t *testing.T) {
	subs := NewSubscriptions()
	sub := &invv1alpha1.Subscription{
		Spec: invv1alpha1.SubscriptionSpec{
			Encoding: ptr.To(invv1alpha1.Encoding_ASCII),
			Subscriptions: []invv1alpha1.SubscriptionParameters{{
				Name:       "s",
				Mode:       "onChange",
				Paths:      []string{"/z", "/a", "/m", "/b"},
				AdminState: ptr.To(invv1alpha1.AdminState_ENABLED),
			}},
		},
	}
	sub.SetNamespace("default")
	sub.SetName("s")
	require.NoError(t, subs.AddSubscription(sub))

	paths := subs.GetPaths()[invv1alpha1.Encoding_ASCII]
	require.Len(t, paths, 4)
	for i := 1; i < len(paths); i++ {
		assert.Less(t, paths[i-1].Path, paths[i].Path, "paths must be sorted")
	}
}
