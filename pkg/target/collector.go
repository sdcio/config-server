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
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/henderiw/store"
	"github.com/henderiw/store/memory"
	"github.com/openconfig/gnmic/pkg/api"
	"github.com/openconfig/gnmic/pkg/api/target"
	"github.com/openconfig/gnmic/pkg/cache"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
)

type Collector struct {
	targetKey          storebackend.Key
	target             *target.Target
	subChan            chan struct{}
	subscriptions      *Subscriptions
	intervalCollectors store.Storer[*IntervalCollector]
	cache              cache.Cache

	m      sync.RWMutex
	port   uint
	cancel context.CancelFunc
}

func NewCollector(targetKey storebackend.Key, subscriptions *Subscriptions) *Collector {
	cache, _ := cache.New(&cache.Config{Type: cache.CacheType("oc")})
	return &Collector{
		targetKey:          targetKey,
		subscriptions:      subscriptions,
		subChan:            make(chan struct{}),
		intervalCollectors: memory.NewStore[*IntervalCollector](nil),
		cache:              cache,
	}
}

func (r *Collector) GetUpdateChan() chan struct{} {
	return r.subChan
}

func (r *Collector) SetPort(port uint) {
	r.m.Lock()
	defer r.m.Unlock()
	r.port = port
}

func (r *Collector) getPort() uint {
	r.m.RLock()
	defer r.m.RUnlock()
	return r.port
}

func (r *Collector) IsRunning() bool {
	r.m.RLock()
	defer r.m.RUnlock()
	return r.cancel != nil
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

	if r.target != nil {
		r.target.Close() // ignore error
		r.target = nil
	}
}

func (r *Collector) Start(ctx context.Context, req *sdcpb.CreateDataStoreRequest) error {
	r.Stop(ctx)
	// don't lock before since stop also locks
	r.m.Lock()
	defer r.m.Unlock()
	ctx, r.cancel = context.WithCancel(ctx)

	log := log.FromContext(ctx).With("name", "targetCollector", "target", r.targetKey.String())
	tOpts := []api.TargetOption{
		api.Name(r.targetKey.String()),
		api.Address(fmt.Sprintf("%s:%d", req.Target.Address, r.port)),
		api.Username(string(req.Target.Credentials.Username)),
		api.Password(string(req.Target.Credentials.Password)),
		api.Timeout(5 * time.Second),
	}
	if req.Target.Tls == nil {
		tOpts = append(tOpts, api.Insecure(true))
	} else {
		tOpts = append(tOpts, api.SkipVerify(req.Target.Tls.SkipVerify))
		tOpts = append(tOpts, api.TLSCA(req.Target.Tls.Ca))
		tOpts = append(tOpts, api.TLSCert(req.Target.Tls.Cert))
		tOpts = append(tOpts, api.TLSKey(req.Target.Tls.Key))
	}
	var err error
	r.target, err = api.NewTarget(tOpts...)
	if err != nil {
		log.Error("cannot create gnmi target", "err", err)
		return err
	}
	if err := r.target.CreateGNMIClient(ctx); err != nil {
		log.Error("cannot create gnmi client", "err", err)
		return err
	}

	go r.start(ctx)

	return nil
}

func (r *Collector) start(ctx context.Context) {
	log := log.FromContext(ctx).With("name", "targetCollector", "target", r.targetKey.String())
	log.Info("start")

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
			intervalCollector := NewIntervalCollector(r.targetKey, interval, paths, r.target, r.cache)
			intervalCollector.Start(ctx)
			r.intervalCollectors.Apply(key, intervalCollector) // ignoring error
		}
	}
}
