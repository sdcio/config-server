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

package configblame

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/henderiw/apiserver-store/pkg/rest"
	watchermanager "github.com/henderiw/apiserver-store/pkg/watcher-manager"
	"github.com/henderiw/logger/log"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
)

// watcher implements watch.Interface for one client and
// watchermanager.Watcher to receive events from the watcherManager.
//
// Lifecycle: Stop() cancels ctx. Every send selects on ctx, so a cancelled
// watch never blocks on resultChan, OnChange releases the lock, setDone()
// completes, and only then is resultChan closed (no send-on-closed panic).
type watcher struct {
	cancel         func()
	resultChan     chan watch.Event
	watcherManager watchermanager.WatcherManager

	// protects eventCallback and done
	m             sync.Mutex
	eventCallback func(eventType watch.EventType, obj runtime.Object) bool
	done          bool
}

var _ watch.Interface = &watcher{}
var _ watchermanager.Watcher = &watcher{}

// Stop stops watching. ResultChan is closed once the watch goroutine exits.
func (r *watcher) Stop() { r.cancel() }

// ResultChan returns the event channel; it is closed when the watch ends.
func (r *watcher) ResultChan() <-chan watch.Event { return r.resultChan }

// OnChange is called by the watcherManager for every change.
func (r *watcher) OnChange(eventType watch.EventType, obj runtime.Object) bool {
	r.m.Lock()
	defer r.m.Unlock()
	if r.eventCallback == nil {
		return !r.done
	}
	return r.eventCallback(eventType, obj)
}

func (r *watcher) listAndWatch(ctx context.Context, l rest.RESTListStrategy, options *metainternalversion.ListOptions) {
	// deferred in reverse: cancel (unblocks any pending send), setDone
	// (no further callback sends), then close.
	defer close(r.resultChan)
	defer r.setDone()
	defer r.cancel()

	err := r.innerListAndWatch(ctx, l, options)
	if err == nil || ctx.Err() != nil {
		// normal termination: client stopped, timeout, or server shutdown
		return
	}

	log.FromContext(ctx).Info("watch failed", "error", err)
	status := toStatus(err)
	r.sendWatchEvent(ctx, watch.Event{Type: watch.Error, Object: &status})
}

// innerListAndWatch registers with the watcherManager (buffering changes in a
// backlog), sends the initial list, replays the backlog and then streams
// changes until ctx is done. It returns nil on normal termination and an
// error only for genuine failures.
func (r *watcher) innerListAndWatch(ctx context.Context, l rest.RESTListStrategy, options *metainternalversion.ListOptions) error {
	log := log.FromContext(ctx)

	// backlog collects events arriving while we list
	var backlog []watch.Event
	r.m.Lock()
	r.eventCallback = func(eventType watch.EventType, obj runtime.Object) bool {
		if r.done {
			return false
		}
		backlog = append(backlog, watch.Event{Type: eventType, Object: obj})
		return true
	}
	r.m.Unlock()

	log.Debug("starting watch")
	if err := r.watcherManager.Add(ctx, options, r); err != nil {
		return apierrors.NewTooManyRequests(err.Error(), 1)
	}

	// options.Watch means watch only, no listing
	if !options.Watch {
		log.Debug("starting list watch")
		obj, err := l.List(ctx, options)
		if err != nil {
			return err
		}
		items, err := meta.ExtractList(obj)
		if err != nil {
			return fmt.Errorf("expecting list type, got: %s, err: %w", reflect.TypeOf(obj).Name(), err)
		}
		for _, obj := range items {
			if !r.sendWatchEvent(ctx, watch.Event{Type: watch.Added, Object: obj}) {
				return nil
			}
		}
		log.Debug("finished list watch")
	} else {
		log.Debug("watch only, no list")
	}

	// repeatedly flush the backlog until we catch up
	for {
		r.m.Lock()
		chunk := backlog
		backlog = nil
		r.m.Unlock()

		if len(chunk) == 0 {
			break
		}
		log.Debug("flushing backlog", "chunk length", len(chunk))
		for _, ev := range chunk {
			if !r.sendWatchEvent(ctx, ev) {
				return nil
			}
		}
	}

	// under the lock: pick up anything that squeezed in, then switch to
	// streaming so no event can fall between the backlog and the stream
	r.m.Lock()
	for _, ev := range backlog {
		if !r.sendWatchEvent(ctx, ev) {
			r.m.Unlock()
			return nil
		}
	}
	backlog = nil

	log.Debug("moving into streaming mode")
	r.eventCallback = func(eventType watch.EventType, obj runtime.Object) bool {
		if r.done {
			return false
		}
		return r.sendWatchEvent(ctx, watch.Event{Type: eventType, Object: obj})
	}
	r.m.Unlock()

	<-ctx.Done()
	return nil
}

// sendWatchEvent delivers ev unless the watch is cancelled first.
// It returns false when the watch is over and the caller should stop.
func (r *watcher) sendWatchEvent(ctx context.Context, ev watch.Event) bool {
	if acc, err := meta.Accessor(ev.Object); err == nil {
		log.FromContext(ctx).Debug("sending watch event", "event", ev.Type,
			"nsn", fmt.Sprintf("%s/%s", acc.GetNamespace(), acc.GetName()))
	} else {
		log.FromContext(ctx).Debug("sending watch event", "event", ev.Type)
	}

	select {
	case <-ctx.Done():
		return false
	case r.resultChan <- ev:
		return true
	}
}

func (r *watcher) setDone() {
	r.m.Lock()
	defer r.m.Unlock()
	r.done = true
}

// toStatus converts err into the metav1.Status client-go expects in an
// ERROR watch event, keeping API errors (code, reason) intact.
func toStatus(err error) metav1.Status {
	var apiStatus apierrors.APIStatus
	if errors.As(err, &apiStatus) {
		return apiStatus.Status()
	}
	return apierrors.NewInternalError(err).ErrStatus
}