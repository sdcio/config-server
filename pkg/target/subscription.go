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
	"fmt"
	"sort"

	"github.com/henderiw/store"
	"github.com/henderiw/store/memory"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"k8s.io/apimachinery/pkg/util/sets"
)

const maxInterval = 1000

var supportIntervals []int = []int{0, 1, 15, 30, 60}

type Subscriptions struct {
	Paths store.Storer[*PathSubscriptions]
}

type PathSubscriptions struct {
	Current *Subscription
	// AllSubscriptions list all the subscriptions that are in the system, the key is name of the subscription CR
	AllSubscriptions map[string]*Subscription
}

type Subscription struct {
	NSN         string // namespacedname of the CR that originated this subscription
	Name        string // name of the subscription
	Description *string
	Labels      map[string]string
	Enabled     bool
	Interval    int // 0 = onChange
	Encoding    invv1alpha1.Encoding
	//Outputs []output -> this should be an interface
}

func NewSubscriptions() *Subscriptions {
	return &Subscriptions{
		Paths: memory.NewStore[*PathSubscriptions](nil),
	}
}

func (r *Subscriptions) HasSubscriptions() bool {
	return r.Paths.Len() != 0
}

// Add or update a subscription
func (r *Subscriptions) AddSubscription(subscription *invv1alpha1.Subscription) error {
	subscriptionNSN := subscription.GetNamespacedName().String()
	var errs error

	// check update of the subscription by validating the existing subscriptions against the cache
	existingCRSubscriptions := r.getExistingCRSubscription(subscriptionNSN)
	for _, subParam := range subscription.Spec.Subscriptions {
		for _, path := range subParam.Paths {
			key := store.ToKey(path)
			// we could have the same path defined in the subscription accross multiple subscriptions
			subscriptionNSNName := getSubscriptionNSNName(subscriptionNSN, subParam.Name)
			// remove the item from the exesting list otherwise it will be deleted
			existingCRPathSubscriptionSet, ok := existingCRSubscriptions[path]
			if ok {
				existingCRPathSubscriptionSet.Delete(subscriptionNSNName)
				existingCRSubscriptions[path] = existingCRPathSubscriptionSet
				if len(existingCRPathSubscriptionSet) == 0 {
					delete(existingCRPathSubscriptionSet, path)
				}
			}

			// convert the subscriptions parameters to a subscription we manage in this cache
			subscription := getSubscription(subscriptionNSN, subParam, subscription.GetEncoding())
			// check if the path exists in the path subscriptions
			pathSubscriptions, err := r.Paths.Get(key)
			if err != nil {
				// does not exist
				pathSubscriptions = &PathSubscriptions{
					AllSubscriptions: map[string]*Subscription{
						subscriptionNSN: subscription,
					},
				}
				if subscription.Enabled {
					pathSubscriptions.Current = subscription
				}
				if err := r.Paths.Apply(key, pathSubscriptions); err != nil {
					errs = errors.Join(errs, err)
				}
				continue
			}
			pathSubscriptions.AllSubscriptions[subscriptionNSNName] = subscription
			// check if we need to update current
			if subscription.Enabled {
				if pathSubscriptions.Current == nil {
					// if no current subscription exists and we get an enabled subsription this subscription becomes current
					pathSubscriptions.Current = subscription
				} else {
					// it can be that the parameters changed
					if subscriptionNSNName == getSubscriptionNSNName(pathSubscriptions.Current.NSN, pathSubscriptions.Current.Name) {
						pathSubscriptions.Current = subscription
					}
					// current exists
					if subscription.Interval < pathSubscriptions.Current.Interval {
						pathSubscriptions.Current = subscription
					}
				}
			}

			// ignoring error for now
			if err := r.Paths.Apply(key, pathSubscriptions); err != nil {
				errs = errors.Join(errs, err)
			}
		}
	}

	for path, existingCRPathSubscriptionSet := range existingCRSubscriptions {
		// TODO delete these
		for _, subscriptionNSNName := range existingCRPathSubscriptionSet.UnsortedList() {
			key := store.ToKey(path)
			pathSubscriptions, err := r.Paths.Get(key)
			if err != nil {
				// strange since we just listed them
				continue
			}
			delete(pathSubscriptions.AllSubscriptions, subscriptionNSN)
			if len(pathSubscriptions.AllSubscriptions) == 0 {
				// if no subscriptions exist we can delete the path from the cache
				if err := r.Paths.Delete(key); err != nil {
					errs = errors.Join(errs, err)
				}
				continue
			}
			// pick a new current subscription since this was the current one
			if pathSubscriptions.Current != nil && subscriptionNSNName == getSubscriptionNSNName(pathSubscriptions.Current.NSN, pathSubscriptions.Current.Name) {
				interval := 1000
				pathSubscriptions.Current = nil
				for _, subscription := range pathSubscriptions.AllSubscriptions {
					if subscription.Interval < interval && subscription.Enabled {
						interval = subscription.Interval
						pathSubscriptions.Current = subscription
					}
				}
			}
		}
	}
	return errs
}

func (r *Subscriptions) DelSubscription(subscription *invv1alpha1.Subscription) error {
	subscriptionNSN := subscription.GetNamespacedName().String()
	var errs error
	for _, subParam := range subscription.Spec.Subscriptions {
		for _, path := range subParam.Paths {
			key := store.ToKey(path)
			// we could have the same path defined in the subscription accross multiple subscriptions
			subscriptionNSNName := getSubscriptionNSNName(subscriptionNSN, subParam.Name)
			pathSubscriptions, err := r.Paths.Get(key)
			if err != nil {
				continue
			}
			delete(pathSubscriptions.AllSubscriptions, subscriptionNSN)
			if len(pathSubscriptions.AllSubscriptions) == 0 {
				// if no subscriptions exist we can delete the path from the cache
				if err := r.Paths.Delete(key); err != nil {
					errs = errors.Join(errs, err)
				}
				continue
			}
			// pick a new current subscription since this was the current one
			if pathSubscriptions.Current != nil && subscriptionNSNName == getSubscriptionNSNName(pathSubscriptions.Current.NSN, pathSubscriptions.Current.Name) {
				interval := maxInterval
				pathSubscriptions.Current = nil
				for _, subscription := range pathSubscriptions.AllSubscriptions {
					if subscription.Interval < interval && subscription.Enabled {
						interval = subscription.Interval
						pathSubscriptions.Current = subscription
					}
				}
			}
		}
	}
	return errs
}

type Path struct {
	Path     string
	Interval int
}

func (r *Subscriptions) GetPaths() map[invv1alpha1.Encoding][]Path {
	encodingPaths := map[invv1alpha1.Encoding][]Path{}
	r.Paths.List(func(k store.Key, subscriptions *PathSubscriptions) {
		if subscriptions.Current != nil {
			paths, ok := encodingPaths[subscriptions.Current.Encoding]
			if !ok {
				paths = make([]Path, 0)
			}
			paths = append(paths, Path{Path: k.Name, Interval: subscriptions.Current.Interval})
			encodingPaths[subscriptions.Current.Encoding] = paths
		}
	})
	for encoding, paths := range encodingPaths {
		sort.Slice(paths, func(i, j int) bool {
			return paths[i].Path < paths[j].Path
		})
		encodingPaths[encoding] = paths
	}

	return encodingPaths
}

func getSubscription(nsn string, param invv1alpha1.SubscriptionParameters, encoding invv1alpha1.Encoding) *Subscription {
	return &Subscription{
		NSN:         nsn,
		Name:        param.Name,
		Description: param.Description,
		Labels:      param.Labels,
		Enabled:     param.IsEnabled(),
		Interval:    param.GetIntervalSeconds(),
		Encoding:    encoding,
	}
}

func (r *Subscriptions) getExistingCRSubscription(nsn string) map[string]sets.Set[string] {
	existingCRSubscriptions := map[string]sets.Set[string]{}
	r.Paths.List(func(k store.Key, ps *PathSubscriptions) {
		for subscriptionNSNName, subscription := range ps.AllSubscriptions {
			if subscription.NSN == nsn {
				if _, ok := existingCRSubscriptions[k.Name]; !ok {
					existingCRSubscriptions[k.Name] = sets.New[string]()
				}
				existingCRSubscriptions[k.Name].Insert(subscriptionNSNName)
			}
		}
	})
	return existingCRSubscriptions
}

func getSubscriptionNSNName(nsn, name string) string {
	return fmt.Sprintf("%s.%s", nsn, name)
}
