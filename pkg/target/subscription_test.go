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
	"k8s.io/utils/ptr"
)

func TestAddSubscription(t *testing.T) {
	subscriptions := NewSubscriptions()

	// Define the first subscription (onChange)
	sub1 := &invv1alpha1.Subscription{
		Spec: invv1alpha1.SubscriptionSpec{
			Encoding: ptr.To(invv1alpha1.Encoding_ASCII),
			Subscriptions: []invv1alpha1.SubscriptionParameters{
				{
					Name:       "sub1",
					Mode:       "onChange",
					Paths:      []string{"/interfaces/interface[name=eth0]/state/counters"},
					AdminState: ptr.To(invv1alpha1.AdminState_ENABLED),
				},
			},
		},
	}
	sub1.SetNamespace("default")
	sub1.SetName("sub1")

	// Define the second subscription (sample, 30s interval)
	sub2 := &invv1alpha1.Subscription{
		Spec: invv1alpha1.SubscriptionSpec{
			Encoding: ptr.To(invv1alpha1.Encoding_ASCII),
			Subscriptions: []invv1alpha1.SubscriptionParameters{
				{
					Name:       "sub2",
					Mode:       "sample",
					Interval:   ptr.To(metav1.Duration{Duration: 30 * time.Second}),
					Paths:      []string{"/interfaces/interface[name=eth0]/state/counters"},
					AdminState: ptr.To(invv1alpha1.AdminState_ENABLED),
				},
			},
		},
	}
	sub2.SetNamespace("default")
	sub2.SetName("sub2")

	// Add the first subscription
	err := subscriptions.AddSubscription(sub1)
	assert.NoError(t, err)

	// Verify that the path exists and is associated with the onChange subscription
	paths := subscriptions.GetPaths()
	assert.Equal(t, []Path{{Path: "/interfaces/interface[name=eth0]/state/counters", Interval: 0}}, paths[invv1alpha1.Encoding_ASCII])

	// Add the second subscription
	err = subscriptions.AddSubscription(sub2)
	assert.NoError(t, err)

	// Verify the path still prioritizes the onChange subscription (interval 0)
	paths = subscriptions.GetPaths()
	assert.Equal(t, []Path{{Path: "/interfaces/interface[name=eth0]/state/counters", Interval: 0}}, paths[invv1alpha1.Encoding_ASCII])

}

func TestDelSubscription(t *testing.T) {
	subscriptions := NewSubscriptions()

	// Define and add a subscription
	sub1 := &invv1alpha1.Subscription{
		Spec: invv1alpha1.SubscriptionSpec{
			Encoding: ptr.To(invv1alpha1.Encoding_ASCII),
			Subscriptions: []invv1alpha1.SubscriptionParameters{
				{
					Name:       "sub1",
					Mode:       "onChange",
					Paths:      []string{"/interfaces/interface[name=eth0]/state/counters"},
					AdminState: ptr.To(invv1alpha1.AdminState_ENABLED),
				},
			},
		},
	}
	sub1.SetNamespace("default")
	sub1.SetName("sub1")

	err := subscriptions.AddSubscription(sub1)
	assert.NoError(t, err)

	// Verify the path exists
	paths := subscriptions.GetPaths()
	assert.Equal(t, []Path{{Path: "/interfaces/interface[name=eth0]/state/counters", Interval: 0}}, paths[invv1alpha1.Encoding_ASCII])

	// Delete the subscription
	err = subscriptions.DelSubscription(sub1)
	assert.NoError(t, err)

	// Verify the path is removed
	paths = subscriptions.GetPaths()
	assert.Empty(t, paths)

	// Verify the internal store no longer contains the path
	_, err = subscriptions.Paths.Get(store.ToKey("/interfaces/interface[name=eth0]/state/counters"))
	assert.Error(t, err)
}

func TestPromotionAfterRemoval(t *testing.T) {
	subscriptions := NewSubscriptions()

	// Define the first subscription (onChange)
	sub1 := &invv1alpha1.Subscription{
		Spec: invv1alpha1.SubscriptionSpec{
			Encoding: ptr.To(invv1alpha1.Encoding_ASCII),
			Subscriptions: []invv1alpha1.SubscriptionParameters{
				{
					Name:       "sub1",
					Mode:       "onChange",
					Paths:      []string{"/interfaces/interface[name=eth0]/state/counters"},
					AdminState: ptr.To(invv1alpha1.AdminState_ENABLED),
				},
			},
		},
	}
	sub1.SetNamespace("default")
	sub1.SetName("sub1")

	// Define the second subscription (sample, 30s interval)
	sub2 := &invv1alpha1.Subscription{
		Spec: invv1alpha1.SubscriptionSpec{
			Encoding: ptr.To(invv1alpha1.Encoding_ASCII),
			Subscriptions: []invv1alpha1.SubscriptionParameters{
				{
					Name:       "sub2",
					Mode:       "sample",
					Interval:   ptr.To(metav1.Duration{Duration: 30 * time.Second}),
					Paths:      []string{"/interfaces/interface[name=eth0]/state/counters"},
					AdminState: ptr.To(invv1alpha1.AdminState_ENABLED),
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

	// Verify that the onChange subscription is prioritized
	paths := subscriptions.GetPaths()
	assert.Equal(t, []Path{{Path: "/interfaces/interface[name=eth0]/state/counters", Interval: 0}}, paths[invv1alpha1.Encoding_ASCII])

	// Delete the onChange subscription
	err = subscriptions.DelSubscription(sub1)
	assert.NoError(t, err)

	// Verify that the 30s subscription is now Current
	paths = subscriptions.GetPaths()
	assert.Equal(t, []Path{{Path: "/interfaces/interface[name=eth0]/state/counters", Interval: 30}}, paths[invv1alpha1.Encoding_ASCII])
}

func TestModifyEncoding(t *testing.T) {
	subscriptions := NewSubscriptions()

	// Define the first subscription (onChange)
	sub1 := &invv1alpha1.Subscription{
		Spec: invv1alpha1.SubscriptionSpec{
			Encoding: ptr.To(invv1alpha1.Encoding_ASCII),
			Subscriptions: []invv1alpha1.SubscriptionParameters{
				{
					Name:       "sub1",
					Mode:       "onChange",
					Paths:      []string{"/interfaces/interface[name=eth0]/state/counters"},
					AdminState: ptr.To(invv1alpha1.AdminState_ENABLED),
				},
			},
		},
	}
	sub1.SetNamespace("default")
	sub1.SetName("sub1")

	// Define the second subscription (sample, 30s interval)
	sub2 := &invv1alpha1.Subscription{
		Spec: invv1alpha1.SubscriptionSpec{
			Encoding: ptr.To(invv1alpha1.Encoding_PROTO),
			Subscriptions: []invv1alpha1.SubscriptionParameters{
				{
					Name:       "sub1",
					Mode:       "onChange",
					Paths:      []string{"/interfaces/interface[name=eth0]/state/counters"},
					AdminState: ptr.To(invv1alpha1.AdminState_ENABLED),
				},
			},
		},
	}
	sub2.SetNamespace("default")
	sub2.SetName("sub1")

	// Add both subscriptions
	err := subscriptions.AddSubscription(sub1)
	assert.NoError(t, err)

	paths := subscriptions.GetPaths()
	assert.Equal(t, []Path{{Path: "/interfaces/interface[name=eth0]/state/counters", Interval: 0}}, paths[invv1alpha1.Encoding_ASCII])


	err = subscriptions.AddSubscription(sub2)
	assert.NoError(t, err)

	// Verify paths for each interval
	paths = subscriptions.GetPaths()
	assert.Equal(t, []Path{{Path: "/interfaces/interface[name=eth0]/state/counters", Interval: 0}}, paths[invv1alpha1.Encoding_PROTO])
}
