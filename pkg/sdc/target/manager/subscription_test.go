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

package targetmanager

import (
	"testing"
	"time"

	"github.com/henderiw/store"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const testPath = "/interfaces/interface[name=eth0]/state/counters"

func newTestSub(crName string, enc invv1alpha1.Encoding, params ...invv1alpha1.SubscriptionParameters) *invv1alpha1.Subscription {
	sub := &invv1alpha1.Subscription{
		Spec: invv1alpha1.SubscriptionSpec{
			Encoding:      ptr.To(enc),
			Subscriptions: params,
		},
	}
	sub.SetNamespace("default")
	sub.SetName(crName)
	return sub
}

func onChange(name string, admin invv1alpha1.AdminState, paths ...string) invv1alpha1.SubscriptionParameters {
	return invv1alpha1.SubscriptionParameters{
		Name:       name,
		Mode:       "onChange",
		Paths:      paths,
		AdminState: ptr.To(admin),
	}
}

func sample30s(name string, paths ...string) invv1alpha1.SubscriptionParameters {
	return invv1alpha1.SubscriptionParameters{
		Name:       name,
		Mode:       "sample",
		Interval:   ptr.To(metav1.Duration{Duration: 30 * time.Second}),
		Paths:      paths,
		AdminState: ptr.To(invv1alpha1.AdminState_ENABLED),
	}
}

func TestAddSubscription(t *testing.T) {
	subscriptions := NewSubscriptions()
	sub1 := newTestSub("sub1", invv1alpha1.Encoding_ASCII, onChange("sub1", invv1alpha1.AdminState_ENABLED, testPath))
	sub2 := newTestSub("sub2", invv1alpha1.Encoding_ASCII, sample30s("sub2", testPath))

	assert.NoError(t, subscriptions.AddSubscription(sub1))
	assert.Equal(t, []Path{{Path: testPath, Interval: 0}}, subscriptions.GetPaths()[invv1alpha1.Encoding_ASCII])

	assert.NoError(t, subscriptions.AddSubscription(sub2))
	// onChange (interval 0) keeps priority
	assert.Equal(t, []Path{{Path: testPath, Interval: 0}}, subscriptions.GetPaths()[invv1alpha1.Encoding_ASCII])
}

func TestDelSubscription(t *testing.T) {
	subscriptions := NewSubscriptions()
	sub1 := newTestSub("sub1", invv1alpha1.Encoding_ASCII, onChange("sub1", invv1alpha1.AdminState_ENABLED, testPath))

	for i := 0; i < 3; i++ {
		assert.NoError(t, subscriptions.AddSubscription(sub1))
	}
	assert.Equal(t, []Path{{Path: testPath, Interval: 0}}, subscriptions.GetPaths()[invv1alpha1.Encoding_ASCII])

	assert.NoError(t, subscriptions.DelSubscription(sub1))
	assert.Empty(t, subscriptions.GetPaths())
	assert.False(t, subscriptions.HasSubscriptions())

	_, err := subscriptions.Paths.Get(store.ToKey(testPath))
	assert.Error(t, err)
}

func TestPromotionAfterRemoval(t *testing.T) {
	subscriptions := NewSubscriptions()
	sub1 := newTestSub("sub1", invv1alpha1.Encoding_ASCII, onChange("sub1", invv1alpha1.AdminState_ENABLED, testPath))
	sub2 := newTestSub("sub2", invv1alpha1.Encoding_ASCII, sample30s("sub2", testPath))

	assert.NoError(t, subscriptions.AddSubscription(sub1))
	assert.NoError(t, subscriptions.AddSubscription(sub2))
	assert.Equal(t, []Path{{Path: testPath, Interval: 0}}, subscriptions.GetPaths()[invv1alpha1.Encoding_ASCII])

	assert.NoError(t, subscriptions.DelSubscription(sub1))
	assert.Equal(t, []Path{{Path: testPath, Interval: 30}}, subscriptions.GetPaths()[invv1alpha1.Encoding_ASCII])
}

func TestModifyEncoding(t *testing.T) {
	subscriptions := NewSubscriptions()
	sub1 := newTestSub("sub1", invv1alpha1.Encoding_ASCII, onChange("sub1", invv1alpha1.AdminState_ENABLED, testPath))
	sub1Proto := newTestSub("sub1", invv1alpha1.Encoding_PROTO, onChange("sub1", invv1alpha1.AdminState_ENABLED, testPath))

	assert.NoError(t, subscriptions.AddSubscription(sub1))
	assert.Equal(t, []Path{{Path: testPath, Interval: 0}}, subscriptions.GetPaths()[invv1alpha1.Encoding_ASCII])

	assert.NoError(t, subscriptions.AddSubscription(sub1Proto))
	paths := subscriptions.GetPaths()
	assert.Equal(t, []Path{{Path: testPath, Interval: 0}}, paths[invv1alpha1.Encoding_PROTO])
	assert.Empty(t, paths[invv1alpha1.Encoding_ASCII])
}

// A path dropped from the CR spec must be pruned on the next Add.
func TestPrunePathRemovedFromSpec(t *testing.T) {
	const other = "/system/name"
	subscriptions := NewSubscriptions()

	assert.NoError(t, subscriptions.AddSubscription(
		newTestSub("sub1", invv1alpha1.Encoding_ASCII, onChange("sub1", invv1alpha1.AdminState_ENABLED, testPath, other))))
	assert.Len(t, subscriptions.GetPaths()[invv1alpha1.Encoding_ASCII], 2)

	assert.NoError(t, subscriptions.AddSubscription(
		newTestSub("sub1", invv1alpha1.Encoding_ASCII, onChange("sub1", invv1alpha1.AdminState_ENABLED, testPath))))
	assert.Equal(t, []Path{{Path: testPath, Interval: 0}}, subscriptions.GetPaths()[invv1alpha1.Encoding_ASCII])

	_, err := subscriptions.Paths.Get(store.ToKey(other))
	assert.Error(t, err)
}

// Disabling the current subscription must promote the next best one.
func TestDisableCurrentPromotes(t *testing.T) {
	subscriptions := NewSubscriptions()
	assert.NoError(t, subscriptions.AddSubscription(
		newTestSub("sub1", invv1alpha1.Encoding_ASCII, onChange("sub1", invv1alpha1.AdminState_ENABLED, testPath))))
	assert.NoError(t, subscriptions.AddSubscription(
		newTestSub("sub2", invv1alpha1.Encoding_ASCII, sample30s("sub2", testPath))))

	assert.NoError(t, subscriptions.AddSubscription(
		newTestSub("sub1", invv1alpha1.Encoding_ASCII, onChange("sub1", invv1alpha1.AdminState_DISABLED, testPath))))
	assert.Equal(t, []Path{{Path: testPath, Interval: 30}}, subscriptions.GetPaths()[invv1alpha1.Encoding_ASCII])
}

// Only-disabled subscriptions: known, but nothing for the collector to do.
func TestNoActivePathsWhenAllDisabled(t *testing.T) {
	subscriptions := NewSubscriptions()
	assert.False(t, subscriptions.HasActivePaths())

	assert.NoError(t, subscriptions.AddSubscription(
		newTestSub("sub1", invv1alpha1.Encoding_ASCII, onChange("sub1", invv1alpha1.AdminState_DISABLED, testPath))))
	assert.True(t, subscriptions.HasSubscriptions())
	assert.False(t, subscriptions.HasActivePaths())
}
