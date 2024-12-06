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

package target

import (
	"testing"
	"time"

	"github.com/henderiw/store"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
)

func TestAddSubscription(t *testing.T) {
	subscriptions := NewSubscriptions()

	// Define the first subscription
	sub1 := &invv1alpha1.Subscription{
		Spec: invv1alpha1.SubscriptionSpec{
			Subscription: []invv1alpha1.SubscriptionSync{
				{
					Mode:  "onChange",
					Paths: []string{"/interfaces/interface[name=eth0]/state/counters"},
				},
			},
		},
	}
	sub1.SetNamespace("default")
	sub1.SetName("sub1")

	// Define the second subscription
	sub2 := &invv1alpha1.Subscription{
		Spec: invv1alpha1.SubscriptionSpec{
			Subscription: []invv1alpha1.SubscriptionSync{
				{
					Mode:     "sample",
					Interval: ptr.To(metav1.Duration{Duration: 30 * time.Second}),
					Paths:    []string{"/interfaces/interface[name=eth0]/state/counters"},
				},
			},
		},
	}
	sub2.SetNamespace("default")
	sub2.SetName("sub2")

	// Add first subscription
	err := subscriptions.AddSubscription(sub1)
	assert.NoError(t, err)

	// Verify the path and interval for the first subscription
	paths := subscriptions.GetPaths(0)
	assert.Equal(t, []string{"/interfaces/interface[name=eth0]/state/counters"}, paths)

	// Add second subscription (higher interval should not override)
	err = subscriptions.AddSubscription(sub2)
	assert.NoError(t, err)

	// Verify the interval remains the lowest (15 seconds)
	paths = subscriptions.GetPaths(0)
	assert.Equal(t, []string{"/interfaces/interface[name=eth0]/state/counters"}, paths)
	paths = subscriptions.GetPaths(30)
	assert.Empty(t, paths)

	// Check the internal data structure for sources
	aggregatedSubscription, err := subscriptions.Paths.Get(store.ToKey("/interfaces/interface[name=eth0]/state/counters"))
	assert.NoError(t, err)
	assert.Equal(t, invv1alpha1.SyncMode("onChange"), aggregatedSubscription.Mode)
	assert.Equal(t, 0, aggregatedSubscription.Interval)
	assert.Equal(t, sets.New[string]("default/sub1", "default/sub2"), aggregatedSubscription.Sources)
}

func TestDelSubscription(t *testing.T) {
	subscriptions := NewSubscriptions()

	// Define a subscription
	sub1 := &invv1alpha1.Subscription{
		Spec: invv1alpha1.SubscriptionSpec{
			Subscription: []invv1alpha1.SubscriptionSync{
				{
					Mode:  "onChange",
					Paths: []string{"/interfaces/interface[name=eth0]/state/counters"},
				},
			},
		},
	}
	sub1.SetNamespace("default")
	sub1.SetName("sub1")

	// Add the subscription
	err := subscriptions.AddSubscription(sub1)
	assert.NoError(t, err)

	// Verify the path exists
	paths := subscriptions.GetPaths(0)
	assert.Equal(t, []string{"/interfaces/interface[name=eth0]/state/counters"}, paths)

	// Delete the subscription
	err = subscriptions.DelSubscription(sub1)
	assert.NoError(t, err)

	// Verify the path is removed
	paths = subscriptions.GetPaths(0)
	assert.Empty(t, paths)

	// Verify the internal store is empty
	_, err = subscriptions.Paths.Get(store.ToKey("/interfaces/interface[name=eth0]/state/counters"))
	assert.Error(t, err)
}

func TestMultipleIntervals(t *testing.T) {
	subscriptions := NewSubscriptions()

	// Define multiple subscriptions with different intervals
	sub1 := &invv1alpha1.Subscription{
		Spec: invv1alpha1.SubscriptionSpec{
			Subscription: []invv1alpha1.SubscriptionSync{
				{
					Mode:  "onChange",
					Paths: []string{"/interfaces/interface[name=eth0]/state/counters"},
				},
			},
		},
	}
	sub1.SetNamespace("default")
	sub1.SetName("sub1")

	sub2 := &invv1alpha1.Subscription{
		Spec: invv1alpha1.SubscriptionSpec{
			Subscription: []invv1alpha1.SubscriptionSync{
				{
					Mode:     "sample",
					Interval: ptr.To(metav1.Duration{Duration: 30 * time.Second}),
					Paths:    []string{"/interfaces/interface[name=eth1]/state/counters"},
				},
			},
		},
	}
	sub2.SetNamespace("default")
	sub2.SetName("sub2")

	// Add both subscriptions
	err := subscriptions.AddSubscription(sub1)
	assert.NoError(t, err)
	err = subscriptions.AddSubscription(sub2)
	assert.NoError(t, err)

	// Verify paths for each interval
	paths15 := subscriptions.GetPaths(0)
	assert.Equal(t, []string{"/interfaces/interface[name=eth0]/state/counters"}, paths15)

	paths30 := subscriptions.GetPaths(30)
	assert.Equal(t, []string{"/interfaces/interface[name=eth1]/state/counters"}, paths30)
}
