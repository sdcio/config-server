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
	errors "errors"
	"fmt"
	"sort"
	"sync"

	"github.com/henderiw/store"
	"github.com/henderiw/store/memory"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"k8s.io/apimachinery/pkg/util/sets"
)

// Subscriptions is written by the subscription reconciler and read by the
// target run loop / collector, so every exported method takes mu.
type Subscriptions struct {
	mu    sync.RWMutex
	Paths store.Storer[*PathSubscriptions]
	// ports holds the port requested by each Subscription CR (key: CR nsn).
	ports map[string]uint
}

type PathSubscriptions struct {
	Current *Subscription
	// AllSubscriptions list all the subscriptions that are in the system, the key is <cr nsn>.<subscription name>
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
		ports: map[string]uint{},
	}
}

// HasSubscriptions reports whether any path is known, enabled or not.
func (r *Subscriptions) HasSubscriptions() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.Paths.Len() != 0
}

// HasActivePaths reports whether at least one path has an enabled subscription,
// i.e. whether there is anything for the collector to subscribe to.
func (r *Subscriptions) HasActivePaths() bool {
	return len(r.GetPaths()) > 0
}

// Port returns the port the collector should dial, 0 if no CR defines one.
// If several CRs define different ports, the CR with the lowest nsn wins so
// the choice is stable across reconciles.
func (r *Subscriptions) Port() uint {
	r.mu.RLock()
	defer r.mu.RUnlock()

	nsns := make([]string, 0, len(r.ports))
	for nsn, p := range r.ports {
		if p != 0 {
			nsns = append(nsns, nsn)
		}
	}
	if len(nsns) == 0 {
		return 0
	}
	sort.Strings(nsns)
	return r.ports[nsns[0]]
}

// AddSubscription adds or updates all subscriptions of a CR, and prunes
// paths/subscriptions the CR previously owned but no longer declares.
func (r *Subscriptions) AddSubscription(subscription *invv1alpha1.Subscription) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	subscriptionNSN := subscription.GetNamespacedName().String()
	r.ports[subscriptionNSN] = uint(subscription.Spec.Port)

	var errs error
	desired := map[string]sets.Set[string]{} // path -> <nsn>.<name> declared now
	for _, subParam := range subscription.Spec.Subscriptions {
		subscriptionNSNName := getSubscriptionNSNName(subscriptionNSN, subParam.Name)
		newSubscription := getSubscription(subscriptionNSN, subParam, subscription.GetEncoding())

		for _, path := range subParam.Paths {
			if desired[path] == nil {
				desired[path] = sets.New[string]()
			}
			desired[path].Insert(subscriptionNSNName)

			key := store.ToKey(path)
			pathSubscriptions, err := r.Paths.Get(key)
			if err != nil {
				pathSubscriptions = &PathSubscriptions{
					AllSubscriptions: make(map[string]*Subscription),
				}
			}
			pathSubscriptions.AllSubscriptions[subscriptionNSNName] = newSubscription
			pathSubscriptions.recomputeCurrent()

			if err := r.Paths.Apply(key, pathSubscriptions); err != nil {
				errs = errors.Join(errs, err)
			}
		}
	}

	// prune what this CR owned before but no longer declares
	for path, owned := range r.getExistingCRSubscription(subscriptionNSN) {
		stale := owned.Difference(desired[path])
		if stale.Len() == 0 {
			continue
		}
		if err := r.removeLocked(path, stale.UnsortedList()...); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}

// DelSubscription removes everything the CR owns. It works on ownership,
// not on the CR spec, so entries from older spec versions are removed too.
func (r *Subscriptions) DelSubscription(subscription *invv1alpha1.Subscription) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	subscriptionNSN := subscription.GetNamespacedName().String()
	delete(r.ports, subscriptionNSN)

	var errs error
	for path, owned := range r.getExistingCRSubscription(subscriptionNSN) {
		if err := r.removeLocked(path, owned.UnsortedList()...); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}

// removeLocked removes the named subscriptions from a path; caller holds mu.
func (r *Subscriptions) removeLocked(path string, subscriptionNSNNames ...string) error {
	key := store.ToKey(path)
	pathSubscriptions, err := r.Paths.Get(key)
	if err != nil {
		return nil // already gone
	}
	for _, n := range subscriptionNSNNames {
		delete(pathSubscriptions.AllSubscriptions, n)
	}
	if len(pathSubscriptions.AllSubscriptions) == 0 {
		return r.Paths.Delete(key)
	}
	pathSubscriptions.recomputeCurrent()
	return r.Paths.Apply(key, pathSubscriptions)
}

// recomputeCurrent picks the enabled subscription with the lowest interval
// (0 = onChange wins). Ties are broken by key so the result is deterministic.
func (ps *PathSubscriptions) recomputeCurrent() {
	names := make([]string, 0, len(ps.AllSubscriptions))
	for n := range ps.AllSubscriptions {
		names = append(names, n)
	}
	sort.Strings(names)

	ps.Current = nil
	for _, n := range names {
		s := ps.AllSubscriptions[n]
		if !s.Enabled {
			continue
		}
		if ps.Current == nil || s.Interval < ps.Current.Interval {
			ps.Current = s
		}
	}
}

type Path struct {
	Path     string
	Interval int
}

// GetPaths returns the active (Current) paths per encoding, sorted by path.
func (r *Subscriptions) GetPaths() map[invv1alpha1.Encoding][]Path {
	r.mu.RLock()
	defer r.mu.RUnlock()

	encodingPaths := map[invv1alpha1.Encoding][]Path{}
	r.Paths.List(func(k store.Key, subscriptions *PathSubscriptions) {
		if subscriptions.Current != nil {
			enc := subscriptions.Current.Encoding
			encodingPaths[enc] = append(encodingPaths[enc], Path{Path: k.Name, Interval: subscriptions.Current.Interval})
		}
	})
	for _, paths := range encodingPaths {
		sort.Slice(paths, func(i, j int) bool {
			return paths[i].Path < paths[j].Path
		})
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

// getExistingCRSubscription returns path -> set(<nsn>.<name>) owned by the CR; caller holds mu.
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
