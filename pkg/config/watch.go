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

package config

/*
import (
	"sync"

	"k8s.io/apimachinery/pkg/watch"
)

func NewWatchers(maxWatchers int) *watchers {
	return &watchers{
		watchers:    make(map[int]*mWatch, maxWatchers),
		idAllocator: *NewIDAllocator(maxWatchers),
	}
}

type watchers struct {
	m           sync.RWMutex
	watchers    map[int]*mWatch
	idAllocator IDAllocator
}

func (r *watchers) IsExhausted() bool {
	return r.idAllocator.IsExhausted()
}

func (r *watchers) Len() int {
	r.m.RLock()
	defer r.m.RUnlock()
	return len(r.watchers)
}

func (r *watchers) Add(w *mWatch) error {
	r.m.Lock()
	defer r.m.Unlock()
	var err error
	w.id, err = r.idAllocator.AllocateID()
	if err != nil {
		return err
	}
	r.watchers[w.id] = w
	return nil
}

func (r *watchers) Del(id int) {
	r.m.Lock()
	defer r.m.Unlock()
	r.idAllocator.ReleaseID(id)
	delete(r.watchers, id)
}

func (r *watchers) NotifyWatchers(ev watch.Event) {
	r.m.RLock()
	defer r.m.RUnlock()
	for _, w := range r.watchers {
		w.resultCh <- ev
	}
}

type mWatch struct {
	watchers *watchers
	resultCh chan watch.Event
	id       int
}

func (r *mWatch) Stop() {
	r.watchers.Del(r.id)
}

func (r *mWatch) ResultChan() <-chan watch.Event {
	return r.resultCh
}
*/
