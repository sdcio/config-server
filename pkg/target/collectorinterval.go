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
	"strings"
	"sync"
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/openconfig/gnmi/proto/gnmi"
	gapi "github.com/openconfig/gnmic/pkg/api"
	"github.com/openconfig/gnmic/pkg/api/target"
	"github.com/openconfig/gnmic/pkg/cache"
)

type IntervalCollector struct {
	targetKey storebackend.Key
	interval  int
	target    *target.Target
	encoding  string
	cache     cache.Cache

	m            sync.RWMutex
	cancel       context.CancelFunc
	paths        []string
	pathsChanged bool
}

func NewIntervalCollector(targetKey storebackend.Key, interval int, paths []string, target *target.Target, cache cache.Cache) *IntervalCollector {
	return &IntervalCollector{
		targetKey: targetKey,
		interval:  interval,
		paths:     paths,
		target:    target,
		cache:     cache,
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
		r.setNewPaths(paths)
		// update cancel
		r.m.Lock()
		defer r.m.Unlock()
		ctx, r.cancel = context.WithCancel(ctx)
		go r.startOnChangeCollector(ctx)
		return
	}
	// update paths -> the ticker will pick them up
	r.setNewPaths(paths)
}

func (r *IntervalCollector) startOnChangeCollector(ctx context.Context) {
	log := log.FromContext(ctx).With("target", r.targetKey.String())
	log.Info("starting onChange collector", "paths", r.paths)

START:
	// subscribe

	opts := make([]gapi.GNMIOption, 0)
	subscriptionOpts := make([]gapi.GNMIOption, 0)
	for _, path := range r.paths {
		subscriptionOpts = append(subscriptionOpts, gapi.Path(path))
	}
	subscriptionOpts = append(subscriptionOpts, gapi.SubscriptionModeON_CHANGE())
	opts = append(opts,
		gapi.EncodingCustom(encoding(r.encoding)),
		gapi.SubscriptionListModeSTREAM(),
		gapi.Subscription(subscriptionOpts...),
	)
	subReq, err := gapi.NewSubscribeRequest(opts...)
	log.Info("subscription onchange request", "req", subReq)

	if err != nil {
		log.Error("subscription onchange failed", "err", err)
		time.Sleep(5 * time.Second)
		goto START
	}
	go r.target.Subscribe(ctx, subReq, "configServer onChange")

	// stop the subscriptions once stopped
	defer r.target.StopSubscriptions()
	rspch, errCh := r.target.ReadSubscriptions()

	for {
		select {
		case <-ctx.Done():
			log.Info("onChange collector stopped")
			return
		case rsp := <-rspch:
			log.Info("onchange subscription update", "update", rsp.Response)
			switch rsp := rsp.Response.ProtoReflect().Interface().(type) {
			case *gnmi.SubscribeResponse:
				switch rsp := rsp.GetResponse().(type) {
				case *gnmi.SubscribeResponse_Update:
					if rsp.Update.GetPrefix() == nil {
						rsp.Update.Prefix = new(gnmi.Path)
					}
					if rsp.Update.GetPrefix().GetTarget() == "" {
						rsp.Update.Prefix.Target = r.targetKey.String()
					}
				}
			}

			r.cache.Write(ctx, "onchange", rsp.Response)
		case err := <-errCh:
			if err.Err != nil {
				r.target.StopSubscriptions()
				log.Error("subscription failed", "err", err)
				time.Sleep(time.Second)
				goto START
			}
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

START:
	// subscribe
	opts := make([]gapi.GNMIOption, 0)
	subscriptionOpts := make([]gapi.GNMIOption, 0)
	for _, path := range r.paths {
		subscriptionOpts = append(subscriptionOpts, gapi.Path(path))
	}
	subscriptionOpts = append(subscriptionOpts, gapi.SubscriptionModeSAMPLE())
	opts = append(opts,
		gapi.EncodingCustom(encoding(r.encoding)),
		gapi.SubscriptionListModeSTREAM(),
		gapi.SampleInterval(time.Duration(r.interval) * time.Second),
		gapi.Subscription(subscriptionOpts...),
	)
	subReq, err := gapi.NewSubscribeRequest(opts...)
	log.Info("subscription sample request", "req", subReq)

	if err != nil {
		log.Error("subscription failed", "err", err)
		time.Sleep(5 * time.Second)
		goto START
	}
	go r.target.Subscribe(ctx, subReq, "configServer sample")

	defer r.target.StopSubscriptions()
	rspch, errCh := r.target.ReadSubscriptions()

	for {
		select {
		case <-ctx.Done():
			log.Info("sampled collector stopped")
			return
		case <-ticker.C:
			if r.getPathsChanged() {
				// we stop the subscription at the time interval and restart
				log.Info("subscribe again to sampled data since paths changed", "paths", r.paths)
				r.target.StopSubscriptions()
				r.setPathsChanged(false)
				goto START
			}
			// dont do anything since the paths have not changed and subscription is enabled
		case rsp := <-rspch:
			log.Info("sample subscription update", "update", rsp.Response)
			switch rsp := rsp.Response.ProtoReflect().Interface().(type) {
			case *gnmi.SubscribeResponse:
				switch rsp := rsp.GetResponse().(type) {
				case *gnmi.SubscribeResponse_Update:
					if rsp.Update.GetPrefix() == nil {
						rsp.Update.Prefix = new(gnmi.Path)
					}
					if rsp.Update.GetPrefix().GetTarget() == "" {
						rsp.Update.Prefix.Target = r.targetKey.String()
					}
				}
			}

			r.cache.Write(ctx, "sampled", rsp.Response)
		case err := <-errCh:
			if err.Err != nil {
				r.target.StopSubscriptions()
				log.Error("subscription failed", "err", err)
				time.Sleep(time.Second)
				goto START
			}
		}
	}
}

func (r *IntervalCollector) setNewPaths(paths []string) {
	r.m.Lock()
	defer r.m.Unlock()
	r.pathsChanged = true
	r.paths = paths
}

func (r *IntervalCollector) setPathsChanged(v bool) {
	r.m.Lock()
	defer r.m.Unlock()
	r.pathsChanged = v
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

func encoding(e string) int {
	enc, ok := gnmi.Encoding_value[strings.ToUpper(e)]
	if ok {
		return int(enc)
	}
	en, err := strconv.Atoi(e)
	if err != nil {
		return 0
	}
	return en
}
