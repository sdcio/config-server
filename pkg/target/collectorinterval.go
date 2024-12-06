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
	"context"
	"sync"
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
)

type IntervalCollector struct {
	targetKey storebackend.Key
	interval  int

	m            sync.RWMutex
	cancel       context.CancelFunc
	paths        []string
	pathsChanged bool
}

func NewIntervalCollector(targetKey storebackend.Key, interval int, paths []string) *IntervalCollector {
	return &IntervalCollector{
		targetKey: targetKey,
		interval:  interval,
		paths:     paths,
	}
}

func (r *IntervalCollector) Stop() {
	r.m.Lock()
	defer r.m.Unlock()
	if r.cancel != nil {
		r.cancel()
	}
}

func (r *IntervalCollector) Start(ctx context.Context) {
	r.Stop()
	// don't lock before since stop also locks
	r.m.Lock()
	defer r.m.Unlock()
	ctx, r.cancel = context.WithCancel(ctx)
	if r.interval == 0 {
		go r.startOnChangeCollector(ctx)
	} else {
		go r.startSampledCollector(ctx)
	}
}

func (r *IntervalCollector) Update(ctx context.Context, paths []string) {
	if !r.hasPathsChanged(paths) {
		return
	}
	if r.interval == 0 {
		r.Stop()
		// don't lock before since stop also locks
		r.m.Lock()
		defer r.m.Unlock()
		r.paths = paths
		ctx, r.cancel = context.WithCancel(ctx)
		go r.startOnChangeCollector(ctx)
		return
	}
	// update paths -> the ticker will pick them up
	r.m.Lock()
	defer r.m.Unlock()
	r.pathsChanged = true
	r.paths = paths
}

func (r *IntervalCollector) startOnChangeCollector(ctx context.Context) {
	log := log.FromContext(ctx).With("target", r.targetKey.String())
	log.Info("starting onChange collector", "paths", r.paths)

	for {
		select {
		case <-ctx.Done():
			log.Info("onChange collector stopped")
			return
		default:
			// TODO subscribe
			log.Info("on change collection", "paths", r.paths)
			time.Sleep(5 * time.Second)
		}
	}
}

func (r *IntervalCollector) startSampledCollector(ctx context.Context) {
	log := log.FromContext(ctx).With("interval", r.interval, "target", r.targetKey.String())

	// Align to clock
	now := time.Now()
	nextTick := now.Truncate(time.Duration(r.interval) * time.Second).Add(time.Duration(r.interval) * time.Second)
	time.Sleep(time.Until(nextTick))

	ticker := time.NewTicker(time.Duration(r.interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("sampled collector stopped")
			return
		case <-ticker.C:
			if r.getPathsChanged() {
				log.Info("subscribe again to sampled data since paths changed", "paths", r.paths)
			}
		}
	}
}

func (r *IntervalCollector) getPathsChanged() bool {
	r.m.RLock()
	defer r.m.RUnlock()
	return r.pathsChanged
}

func (r *IntervalCollector) hasPathsChanged(a []string) bool {
	r.m.RLock()
	defer r.m.RUnlock()
	b := r.paths
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
