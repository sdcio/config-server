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

package generic

import (
	"context"
	"fmt"
	"strconv"
	"sync/atomic"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	watchermanager "github.com/henderiw/apiserver-store/pkg/watcher-manager"
	"github.com/henderiw/logger/log"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
)

// watchQueueSize bounds the events buffered for one watch client. A client
// that falls further behind has its watch closed; it re-watches and, being
// behind, gets 410 Gone and relists (the standard Kubernetes behaviour).
const watchQueueSize = 1024

// watcher implements watch.Interface for one client and
// watchermanager.Watcher to receive events from the watcherManager.
type watcher struct {
	cancel         context.CancelFunc
	resultChan     chan watch.Event
	watcherManager watchermanager.WatcherManager

	queue    chan watch.Event
	overflow atomic.Bool
}

var _ watch.Interface = &watcher{}
var _ watchermanager.Watcher = &watcher{}

func newWatcher(cancel context.CancelFunc, wm watchermanager.WatcherManager) *watcher {
	return &watcher{
		cancel:         cancel,
		resultChan:     make(chan watch.Event),
		watcherManager: wm,
		queue:          make(chan watch.Event, watchQueueSize),
	}
}

func (w *watcher) Stop() { w.cancel() }

func (w *watcher) ResultChan() <-chan watch.Event { return w.resultChan }

// OnChange is called by the watcherManager, while the writer holds the
// resourceVersion lock. It must never block.
func (w *watcher) OnChange(eventType watch.EventType, obj runtime.Object) bool {
	select {
	case w.queue <- watch.Event{Type: eventType, Object: obj}:
		return true
	default:
		w.overflow.Store(true)
		w.cancel()
		return false
	}
}

// listAndWatch implements the Kubernetes watch semantics:
//
//	resourceVersion "" / "0"            -> current state as ADDED, then changes
//	resourceVersion == current          -> changes after it
//	resourceVersion != current          -> 410 Gone (client relists)
//	sendInitialEvents=true (watch-list) -> current state as ADDED, then a
//	                                       bookmark marking the end of the
//	                                       initial events, then changes
//
// There is no event history yet, so resuming from an older resourceVersion
// is answered with 410 instead of a replay. That is correct, it only costs a
// relist; a small ring buffer of recent events can be added later.
func (w *watcher) listAndWatch(ctx context.Context, r *strategy, options *metainternalversion.ListOptions) {
	defer close(w.resultChan)
	log := log.FromContext(ctx).With("resource", r.gr.String(), "rv", options.ResourceVersion)

	selected, err := r.predicate(ctx, options)
	if err != nil {
		w.sendError(ctx, apierrors.NewBadRequest(err.Error()))
		return
	}

	// 1) subscribe BEFORE reading the current resourceVersion, so no write
	//    can fall between the starting point and the stream
	if err := w.watcherManager.Add(ctx, options, w); err != nil {
		w.sendError(ctx, apierrors.NewTooManyRequests(err.Error(), 1))
		return
	}

	// 2) starting point
	r.initRV(ctx)
	start := r.rv.Current()

	rv := options.ResourceVersion
	sendInitial := rv == "" || rv == "0"
	watchList := options.SendInitialEvents != nil && *options.SendInitialEvents
	if options.SendInitialEvents != nil {
		sendInitial = *options.SendInitialEvents
	}

	if !sendInitial && rv != "" && rv != "0" {
		n, err := strconv.ParseUint(rv, 10, 64)
		if err != nil {
			w.sendError(ctx, apierrors.NewBadRequest(fmt.Sprintf("invalid resourceVersion %q", rv)))
			return
		}
		if n != start {
			log.Debug("watch resourceVersion not current, client must relist", "current", start)
			w.sendError(ctx, apierrors.NewResourceExpired(
				fmt.Sprintf("too old resource version: %d (%d)", n, start)))
			return
		}
	}

	// 3) initial state
	if sendInitial {
		// collect first: never send while the storage iterator is open
		var objs []runtime.Object
		if err := r.storage.List(ctx, func(ctx context.Context, _ storebackend.Key, obj runtime.Object) {
			if selected(ctx, obj) {
				objs = append(objs, obj)
			}
		}); err != nil {
			w.sendError(ctx, apierrors.NewInternalError(err))
			return
		}
		for _, obj := range objs {
			if !w.send(ctx, watch.Event{Type: watch.Added, Object: obj}) {
				return
			}
		}
		if watchList {
			if !w.send(ctx, watch.Event{Type: watch.Bookmark, Object: r.initialEventsEndBookmark(start)}) {
				return
			}
		}
	}

	// 4) stream changes after the starting point
	for {
		select {
		case <-ctx.Done():
			if w.overflow.Load() {
				log.Info("watch closed: client too slow")
			}
			return
		case ev := <-w.queue:
			if n, ok := resourceVersionOf(ev.Object); ok && n <= start {
				continue // already covered by the starting point
			}
			if !selected(ctx, ev.Object) {
				continue
			}
			if !w.send(ctx, ev) {
				return
			}
		}
	}
}

// initialEventsEndBookmark is the bookmark that tells a watch-list client
// (sendInitialEvents=true) that the initial state is complete.
func (r *strategy) initialEventsEndBookmark(rv uint64) runtime.Object {
	obj := r.obj.New()
	if acc, err := meta.Accessor(obj); err == nil {
		acc.SetResourceVersion(strconv.FormatUint(rv, 10))
		acc.SetAnnotations(map[string]string{metav1.InitialEventsAnnotationKey: "true"})
	}
	return obj
}

func (w *watcher) send(ctx context.Context, ev watch.Event) bool {
	select {
	case <-ctx.Done():
		return false
	case w.resultChan <- ev:
		return true
	}
}

func (w *watcher) sendError(ctx context.Context, err *apierrors.StatusError) {
	status := err.ErrStatus
	w.send(ctx, watch.Event{Type: watch.Error, Object: &status})
}