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

package watchermanager

import "sync"

type watchers struct {
	m        sync.RWMutex
	watchers map[string]*watcher
}

func newWatchersCache() *watchers {
	return &watchers{
		watchers: map[string]*watcher{},
	}
}

func (r *watchers) add(key string, w *watcher) {
	r.m.Lock()
	defer r.m.Unlock()
	r.watchers[key] = w
}

func (r *watchers) del(key string) {
	r.m.Lock()
	defer r.m.Unlock()
	delete(r.watchers, key)
}

/*
func (r *watchers) get(key string) (*watcher, bool) {
	r.m.RLock()
	defer r.m.RUnlock()
	w, ok := r.watchers[key]
	return w, ok
}
*/

func (r *watchers) len() int {
	r.m.RLock()
	defer r.m.RUnlock()
	return len(r.watchers)
}

func (r *watchers) list() []*watcher {
	r.m.RLock()
	defer r.m.RUnlock()
	ws := make([]*watcher, 0, len(r.watchers))
	for _, w := range r.watchers {
		ws = append(ws, w)
	}
	return ws
}
