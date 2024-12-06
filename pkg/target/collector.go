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

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/henderiw/store"
	"github.com/henderiw/store/memory"
	"github.com/openconfig/gnmic/pkg/api/target"
	"github.com/openconfig/gnmic/pkg/api/types"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"k8s.io/utils/ptr"
)

type Collector struct {
	targetKey          storebackend.Key
	target             *target.Target
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

func (r *Collector) Start(ctx context.Context, req *sdcpb.CreateDataStoreRequest) error {
	r.Stop(ctx)
	// don't lock before since stop also locks
	log := log.FromContext(ctx).With("name", "targetCollector", "target", r.targetKey.String())
	r.m.Lock()
	defer r.m.Unlock()
	ctx, r.cancel = context.WithCancel(ctx)

	// create gnmiClient
	targetConfig := &types.TargetConfig{
		Name:     r.targetKey.String(),
		Address:  fmt.Sprintf("%s:%d", req.Target.Address, req.Target.Port),
		Username: &req.Target.Credentials.Username,
		Password: &req.Target.Credentials.Password,
		Insecure: ptr.To(true),
	}
	if req.Target.Tls != nil {
		targetConfig.Insecure = ptr.To(false)
		targetConfig.TLSCA = ptr.To(req.Target.Tls.Ca)
		targetConfig.TLSCert = ptr.To(req.Target.Tls.Cert)
		targetConfig.TLSKey = ptr.To(req.Target.Tls.Key)

	}
	r.target = target.NewTarget(targetConfig)
	if err := r.target.CreateGNMIClient(ctx); err != nil {
		log.Error("cannot create gnmi collector target", "err", err)
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
			intervalCollector := NewIntervalCollector(r.targetKey, interval, paths, r.target)
			intervalCollector.Start(ctx)
			r.intervalCollectors.Apply(key, intervalCollector) // ignoring error
		}
	}
}
