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
	"strconv"
	"sync"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/henderiw/store"
	"github.com/henderiw/store/memory"
)

type Collector struct {
	targetKey          storebackend.Key
	subChan            chan struct{}
	subscriptions      *Subscriptions
	intervalCollectors store.Storer[*IntervalCollector]

	m      sync.RWMutex
	cancel context.CancelFunc
}

func NewCollector(targetKey storebackend.Key, subscriptions *Subscriptions) *Collector {
	return &Collector{
		targetKey:          targetKey,
		subscriptions:      subscriptions,
		subChan:            make(chan struct{}),
		intervalCollectors: memory.NewStore[*IntervalCollector](nil),
	}
}

func (r *Collector) GetUpdateChan() chan struct{} {
	return r.subChan
}

func (r *Collector) Stop(ctx context.Context) {
	r.m.Lock()
	defer r.m.Unlock()

	r.intervalCollectors.List(func(k store.Key, intervalCollector *IntervalCollector) {
		log := log.FromContext(ctx).With("interval", k.Name, "target", r.targetKey.String())
		log.Info("stopping interval collector")
		if intervalCollector.cancel != nil {
			intervalCollector.cancel()
		}
	})

	for _, key := range r.intervalCollectors.ListKeys() {
		r.intervalCollectors.Delete(store.ToKey(key))
	}

	log := log.FromContext(ctx).With("name", "targetCollector", "target", r.targetKey.String())
	log.Info("stopping target collector")
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
}

func (r *Collector) Start(ctx context.Context) {
	r.Stop(ctx)
	// don't lock before since stop also locks
	r.m.Lock()
	defer r.m.Unlock()
	ctx, r.cancel = context.WithCancel(ctx)
	go r.start(ctx)
}

func (r *Collector) start(ctx context.Context) {
	log := log.FromContext(ctx).With("name", "targetCollector", "target", r.targetKey.String())
	log.Info("start")

	// create gnmiClient
	// kick the collectors
	r.updateIntervalCollectors(ctx)

	for {
		select {
		case <-ctx.Done():
		case <-r.subChan:
			log.Info("subscription update received")
			r.updateIntervalCollectors(ctx)
		}
	}
}

func (r *Collector) updateIntervalCollectors(ctx context.Context) {
	r.m.Lock()
	defer r.m.Unlock()

	for _, interval := range supportIntervals {
		log := log.FromContext(ctx).With("name", "targetCollector", "target", r.targetKey.String(), "interval", interval)
		paths := r.subscriptions.GetPaths(interval)
		key := store.ToKey(strconv.Itoa(interval))

		log.Info("updateIntervalCollectors", "paths", paths)

		if intervalCollector, err := r.intervalCollectors.Get(key); err == nil {
			// interval exists/is running
			if len(paths) == 0 {
				//interval exists/is running; without paths we need to stop and delete the interval
				log.Info("stopping interval collector given no paths exists any longer")
				intervalCollector.Stop()
				r.intervalCollectors.Delete(key)
				continue
			}
			//interval exists/is running; if the paths are different we need to stop/start the collector with new paths
			intervalCollector.Update(ctx, paths)
			r.intervalCollectors.Apply(key, intervalCollector) // ignoring error
			continue
		}

		// Start interval-specific goroutine if not already running
		if len(paths) != 0 {
			log.Info("starting interval collector")
			intervalCollector := NewIntervalCollector(r.targetKey, interval, paths)
			intervalCollector.Start(ctx)
			r.intervalCollectors.Apply(key, intervalCollector) // ignoring error
		}
	}
}
