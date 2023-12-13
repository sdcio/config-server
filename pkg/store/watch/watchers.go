// Copyright 2023 The xxx Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package watch

import (
	"context"
	"fmt"
	"sync"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

func NewWatchers[T1 any](maxWatchers int) *Watchers[T1] {
	return &Watchers[T1]{
		watchers:    make(map[int]*WatchCtx[T1], maxWatchers),
		idAllocator: *newIDAllocator(maxWatchers),
	}
}

type Watchers[T1 any] struct {
	m           sync.RWMutex
	watchers    map[int]*WatchCtx[T1]
	idAllocator idAllocator
}

func (r *Watchers[T1]) GetWatchContext() *WatchCtx[T1] {
	return &WatchCtx[T1]{
		Watchers: r,
		ResultCh: make(chan Event[T1], 10),
	}
}

func (r *Watchers[T1]) IsExhausted() bool {
	return r.idAllocator.IsExhausted()
}

func (r *Watchers[T1]) Len() int {
	r.m.RLock()
	defer r.m.RUnlock()
	return len(r.watchers)
}

func (r *Watchers[T1]) Add(w *WatchCtx[T1]) error {
	r.m.Lock()
	defer r.m.Unlock()
	var err error
	w.id, err = r.idAllocator.AllocateID()
	if err != nil {
		return err
	}
	fmt.Println("added watcher", w.id)
	r.watchers[w.id] = w
	return nil
}

func (r *Watchers[T1]) Del(id int) {
	r.m.Lock()
	defer r.m.Unlock()
	wctx, ok := r.watchers[id]
	if !ok {
		return
	}
	r.idAllocator.ReleaseID(id)
	delete(r.watchers, id)
	// close safely since the client will not get info any longer after the delete
	close(wctx.ResultCh)
}

func (r *Watchers[T1]) NotifyWatchers(ctx context.Context, ev Event[T1]) {
	r.m.RLock()
	defer r.m.RUnlock()
	log := log.FromContext(ctx)
	log.Info("notify watchers", "eventType", ev.Type.String())
	for id, w := range r.watchers {
		log.Info("notify watcher", "eventType", ev.Type.String(), "id", id)
		w.ResultCh <- ev
	}
}
