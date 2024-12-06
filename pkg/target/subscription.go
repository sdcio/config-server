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
	errors "errors"
	"sort"

	"github.com/henderiw/store"
	"github.com/henderiw/store/memory"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"k8s.io/apimachinery/pkg/util/sets"
)

var supportIntervals []int = []int{0, 1, 15, 30, 60}

type AggregatedSubscription struct {
	Mode     invv1alpha1.SyncMode // Final mode: onChange or sample
	Interval int                  // Final interval (only for sample) -> in seconds // 1, 15, 30, 60
	Sources  sets.Set[string]     // Track contributing CRs by their name
}

type Subscriptions struct {
	Paths store.Storer[*AggregatedSubscription] // Path -> AggregatedSubscription
}

func NewTargetSubscriptions() *Subscriptions {
	return &Subscriptions{
		Paths: memory.NewStore[*AggregatedSubscription](nil),
	}
}

// Add or update a subscription
func (r *Subscriptions) AddSubscription(subscription *invv1alpha1.Subscription) (sets.Set[int], error) {
	changedIntervals := sets.New[int]()

	subscriptionNSN := subscription.GetNamespacedName().String()
	var errs error
	for _, sub := range subscription.Spec.Subscription {
		for _, path := range sub.Paths {
			interval := sub.GetIntervalSeconds()
			key := store.ToKey(path)
			aggregatedSubscription, err := r.Paths.Get(key)
			if err != nil {
				// does not exist
				changedIntervals.Insert(interval)
				sources := sets.New[string]()
				sources.Insert(subscriptionNSN)
				// ignoring error for now
				if err := r.Paths.Apply(key, &AggregatedSubscription{
					Mode:     sub.Mode,
					Interval: sub.GetIntervalSeconds(),
					Sources:  sources,
				}); err != nil {
					errs = errors.Join(errs, err)
				}
				continue
			}
			aggregatedSubscription.Sources.Insert(subscriptionNSN)
			if interval < aggregatedSubscription.Interval {
				aggregatedSubscription.Interval = interval
				aggregatedSubscription.Mode = sub.Mode
				changedIntervals.Insert(interval)
			}
			// ignoring error for now
			if err := r.Paths.Apply(key, aggregatedSubscription); err != nil {
				errs = errors.Join(errs, err)
			}
		}
	}
	return changedIntervals, errs
}

func (r *Subscriptions) DelSubscription(subscription *invv1alpha1.Subscription) (sets.Set[int], error) {
	changedIntervals := sets.New[int]()
	subscriptionNSN := subscription.GetNamespacedName().String()
	var errs error
	for _, sub := range subscription.Spec.Subscription {
		for _, path := range sub.Paths {
			key := store.ToKey(path)
			aggregatedSubscription, err := r.Paths.Get(key)
			if err != nil {
				continue
			}
			aggregatedSubscription.Sources.Delete(subscriptionNSN)
			if aggregatedSubscription.Sources.Len() == 0 {
				changedIntervals.Insert(aggregatedSubscription.Interval)
				// ignore error for now
				if err := r.Paths.Delete(key); err != nil {
					errs = errors.Join(errs, err)
				}
			}
		}
	}
	return changedIntervals, errs
}

func (r *Subscriptions) GetPaths(interval int) []string {
	paths := []string{}
	r.Paths.List(func(k store.Key, as *AggregatedSubscription) {
		if as.Interval == interval {
			paths = append(paths, k.Name)
		}
	})
	sort.Strings(paths)
	return paths
}
