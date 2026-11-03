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

package generic

import (
	"context"
	"strconv"
	"sync"

	"github.com/henderiw/logger/log"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
)

// rvAllocator hands out a monotonically increasing resourceVersion for ONE
// GroupResource, shared by every strategy writing that resource (main and
// status). This gives the collection a single ordered history, which is what
// informers (incl. the garbage collector) assume:
//
//   - every write (create, update, status update, delete) takes the next value
//   - List returns the current value
//   - a watch resumes from a value and gets everything after it
//
// Writes are serialized per resource (allocate + store + notify under mu), so
// watch events are emitted in resourceVersion order.
//
// The counter lives in memory. On first use it is initialized to the highest
// resourceVersion found in storage, so it never goes back below a stored
// object. resourceVersions only carried by delete events are not persisted:
// a client that saw one may come back with a value above the counter after a
// restart; the watch answers that with 410 Gone and the client relists.
type rvAllocator struct {
	mu      sync.Mutex
	once    sync.Once
	current uint64
}

var allocators sync.Map // schema.GroupResource -> *rvAllocator

func allocatorFor(gr schema.GroupResource) *rvAllocator {
	a, _ := allocators.LoadOrStore(gr, &rvAllocator{})
	return a.(*rvAllocator)
}

// ensureInit sets the counter to the highest stored resourceVersion, once.
// list visits every stored object.
func (a *rvAllocator) ensureInit(ctx context.Context, list func(visit func(runtime.Object)) error) {
	a.once.Do(func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		var max uint64
		if err := list(func(obj runtime.Object) {
			if n, ok := resourceVersionOf(obj); ok && n > max {
				max = n
			}
		}); err != nil {
			log.FromContext(ctx).Error("resourceVersion init: list failed, starting from 0", "err", err)
		}
		if max > a.current {
			a.current = max
		}
	})
}

// Current returns the resourceVersion of the last completed write. Every
// write with a resourceVersion <= Current is visible in storage.
func (a *rvAllocator) Current() uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.current
}

// write runs fn with the next resourceVersion. fn persists the object and
// returns the watch event; the counter only advances if fn succeeds, and the
// event is published while the lock is held so events stay ordered.
func (a *rvAllocator) write(fn func(rv string) (*watch.Event, error), publish func(watch.Event)) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	next := a.current + 1
	ev, err := fn(strconv.FormatUint(next, 10))
	if err != nil {
		return err
	}
	a.current = next
	if ev != nil && publish != nil {
		publish(*ev)
	}
	return nil
}

func resourceVersionOf(obj runtime.Object) (uint64, bool) {
	acc, err := meta.Accessor(obj)
	if err != nil {
		return 0, false
	}
	n, err := strconv.ParseUint(acc.GetResourceVersion(), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func setResourceVersion(obj runtime.Object, rv string) error {
	acc, err := meta.Accessor(obj)
	if err != nil {
		return err
	}
	acc.SetResourceVersion(rv)
	return nil
}