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
	"strings"
	"sync"
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/openconfig/gnmi/proto/gnmi"
	gapi "github.com/openconfig/gnmic/pkg/api"
	"github.com/openconfig/gnmic/pkg/api/target"
	"github.com/openconfig/gnmic/pkg/cache"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"google.golang.org/protobuf/encoding/prototext"
)

type IntervalCollector struct {
	targetKey storebackend.Key
	interval  int
	target    *target.Target
	cache     cache.Cache

	m            sync.RWMutex
	cancel       context.CancelFunc
	paths        map[invv1alpha1.Encoding][]string
	pathsChanged bool
}

func NewIntervalCollector(targetKey storebackend.Key, interval int, paths map[invv1alpha1.Encoding][]string, target *target.Target, cache cache.Cache) *IntervalCollector {
	return &IntervalCollector{
		targetKey: targetKey,
		interval:  interval,
		paths:     paths,
		target:    target,
		cache:     cache,
	}
}

func (r *IntervalCollector) StopCollector() {
	r.m.Lock()
	defer r.m.Unlock()
	if r.cancel != nil {
		r.cancel()
	}
}

func (r *IntervalCollector) Start(ctx context.Context) {
	r.StopCollector()
	// don't lock before since stop also locks
	r.m.Lock()
	defer r.m.Unlock()
	ctx, r.cancel = context.WithCancel(ctx)
	go r.startCollector(ctx)
}

func (r *IntervalCollector) Update(ctx context.Context, paths map[invv1alpha1.Encoding][]string) {
	if !r.hasPathsChanged(paths) {
		return
	}
	if r.interval == 0 {
		r.StopCollector()
		// don't lock before since stop also locks
		r.setNewPaths(paths)
		// update cancel
		r.m.Lock()
		defer r.m.Unlock()
		ctx, r.cancel = context.WithCancel(ctx)
		go r.startCollector(ctx)
		return
	}
	// update paths -> the ticker will pick them up
	r.setNewPaths(paths)
}

func (r *IntervalCollector) startCollector(ctx context.Context) {
	log := log.FromContext(ctx).With("target", r.targetKey.String())
	collectorType := "onChange"
	if r.interval != 0 {
		collectorType = fmt.Sprintf("interval=%d", r.interval)
	}
	log.Info("starting collector", "type", collectorType, "paths", r.paths)

	var ticker *time.Ticker
	if r.interval != 0 {
		// Create and align the ticker if interval-based sampling is enabled
		now := time.Now()
		nextTick := now.Truncate(time.Duration(r.interval) * time.Second).Add(time.Duration(r.interval) * time.Second)
		time.Sleep(time.Until(nextTick))
		ticker = time.NewTicker(time.Duration(r.interval) * time.Second)
		defer ticker.Stop()
	}

START:
	// subscribe
	for subEncoding, paths := range r.paths {
		opts := make([]gapi.GNMIOption, 0)
		for _, path := range paths {
			subscriptionOpts := []gapi.GNMIOption{
				gapi.Path(path),
			}
			if r.interval == 0 {
				subscriptionOpts = append(subscriptionOpts, gapi.SubscriptionModeON_CHANGE())
			} else {
				subscriptionOpts = append(subscriptionOpts, gapi.SubscriptionModeSAMPLE())
				subscriptionOpts = append(subscriptionOpts, gapi.SampleInterval(time.Duration(r.interval)*time.Second))
			}
			opts = append(opts, gapi.Subscription(subscriptionOpts...))
		}
		opts = append(opts,
			gapi.EncodingCustom(encoding(subEncoding.String())),
			gapi.SubscriptionListModeSTREAM(),
		)
		subReq, err := gapi.NewSubscribeRequest(opts...)
		if err != nil {
			log.Error("subscription failed", "err", err)
			time.Sleep(5 * time.Second)
			goto START
		}
		log.Info("subscription request", "req", prototext.Format(subReq))
		go r.target.Subscribe(ctx, subReq, fmt.Sprintf("configserver %s %d %s", collectorType, r.interval, subEncoding.String()))
	}

	// stop the subscriptions once stopped
	defer r.target.StopSubscriptions()
	rspch, errCh := r.target.ReadSubscriptions()

	// set path changed to false since 
	r.setPathsChanged(false)

	for {
		select {
		case <-ctx.Done():
			log.Info("collector stopped")
			return
		case rsp := <-rspch:
			log.Debug("subscription update", "update", rsp.Response)
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

			r.cache.Write(ctx, collectorType, rsp.Response)
		case err := <-errCh:
			if err.Err != nil {
				r.target.StopSubscriptions()
				log.Error("subscription failed", "err", err)
				time.Sleep(time.Second)
				goto START
			}
		default:
			// Ticker logic is only included if interval != 0
			if ticker != nil {
				select {
				case <-ticker.C:
					if r.getPathsChanged() {
						log.Info("subscribe again to sampled data since paths changed", "paths", r.paths)
						r.target.StopSubscriptions()
						r.setPathsChanged(false)
						goto START
					}
				default:
					// No operation for the ticker's default
				}
			}
		}
	}
}

func (r *IntervalCollector) setNewPaths(paths map[invv1alpha1.Encoding][]string) {
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

func (r *IntervalCollector) hasPathsChanged(newEncodedPaths map[invv1alpha1.Encoding][]string) bool {
	r.m.RLock()
	defer r.m.RUnlock()
	existingEncodedPaths := r.paths
	for encoding, newpaths := range newEncodedPaths {
		existingPaths, ok := existingEncodedPaths[encoding]
		if !ok {
			return false
		}
		if len(newpaths) != len(existingPaths) {
			return false
		}
		for i := range existingPaths {
			if existingPaths[i] != newpaths[i] {
				return false
			}
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
