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
	"github.com/openconfig/gnmic/pkg/api"
	gapi "github.com/openconfig/gnmic/pkg/api"
	"github.com/openconfig/gnmic/pkg/api/target"
	"github.com/openconfig/gnmic/pkg/cache"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/protobuf/encoding/prototext"
)

type Collector struct {
	targetKey     storebackend.Key
	subChan       chan struct{}
	subscriptions *Subscriptions
	//intervalCollectors store.Storer[*IntervalCollector]
	cache cache.Cache

	m      sync.RWMutex
	target *target.Target
	port   uint
	paths  map[invv1alpha1.Encoding][]Path
	cancel context.CancelFunc

	subscriptionCancel context.CancelFunc
}

func NewCollector(targetKey storebackend.Key, subscriptions *Subscriptions) *Collector {
	cache, _ := cache.New(&cache.Config{Type: cache.CacheType("oc")})
	return &Collector{
		targetKey:     targetKey,
		subscriptions: subscriptions,
		subChan:       make(chan struct{}),
		//intervalCollectors: memory.NewStore[*IntervalCollector](nil),
		cache: cache,
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

	log := log.FromContext(ctx).With("name", "targetCollector", "target", r.targetKey.String())
	log.Info("stop collector")
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}

	if r.target != nil {
		r.target.StopSubscriptions()
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
	log.Info("start collector")

	// kick the collectors
	r.setNewPaths(map[invv1alpha1.Encoding][]Path{}) // set the paths to empty since we are just starting the collector
	r.update(ctx)

	for {
		select {
		case <-ctx.Done():
		case <-r.subChan:
			r.update(ctx)
		}
	}
}

func (r *Collector) update(ctx context.Context) {
	log := log.FromContext(ctx).With("name", "targetCollector", "target", r.targetKey.String())
	newPaths := r.subscriptions.GetPaths()
	log.Info("subscription update received", "newPaths", newPaths)
	if !r.hasPathsChanged(newPaths) {
		log.Info("subscription did not change")
		return
	}
	log.Info("subscription did not change", "newPaths", newPaths, "existingPaths", r.paths)
	r.StopSubscription(ctx)

	r.setNewPaths(newPaths)
	// don't lock before since stop also locks
	r.m.Lock()
	defer r.m.Unlock()
	ctx, r.cancel = context.WithCancel(ctx)
	go r.StartSubscription(ctx)
}

func (r *Collector) setNewPaths(paths map[invv1alpha1.Encoding][]Path) {
	r.m.Lock()
	defer r.m.Unlock()
	r.paths = paths
}

func (r *Collector) hasPathsChanged(newPaths map[invv1alpha1.Encoding][]Path) bool {
	r.m.RLock()
	defer r.m.RUnlock()
	existingPaths := r.paths
	for encoding, newpaths := range newPaths {
		existingPaths, ok := existingPaths[encoding]
		if !ok {
			return true
		}
		if len(newpaths) != len(existingPaths) {
			return true
		}
		for i := range existingPaths {
			if existingPaths[i].Path != newpaths[i].Path ||
				existingPaths[i].Interval != newpaths[i].Interval {
				return true
			}
		}
	}
	return false
}

func (r *Collector) StopSubscription(ctx context.Context) {
	log := log.FromContext(ctx).With("name", "targetCollector", "target", r.targetKey.String())
	log.Info("stop subscription")
	r.setNewPaths(nil)
	r.m.Lock()
	defer r.m.Unlock()
	if r.subscriptionCancel != nil {
		r.subscriptionCancel()
		r.subscriptionCancel = nil
	}
}

func (r *Collector) StartSubscription(ctx context.Context) {
	log := log.FromContext(ctx).With("name", "targetCollector", "target", r.targetKey.String())
	log.Info("start subscription")

	r.m.Lock()
	defer r.m.Unlock()
	ctx, r.cancel = context.WithCancel(ctx)
	go r.startSubscription(ctx)
}

func (r *Collector) startSubscription(ctx context.Context) {
	log := log.FromContext(ctx).With("target", r.targetKey.String())
	log.Info("starting collector", "paths", r.paths)

START:
	// subscribe
	for subEncoding, paths := range r.paths {
		opts := make([]gapi.GNMIOption, 0)
		for _, path := range paths {
			subscriptionOpts := []gapi.GNMIOption{
				gapi.Path(path.Path),
			}
			if path.Interval == 0 {
				subscriptionOpts = append(subscriptionOpts, gapi.SubscriptionModeON_CHANGE())
			} else {
				subscriptionOpts = append(subscriptionOpts, gapi.SubscriptionModeSAMPLE())
				subscriptionOpts = append(subscriptionOpts, gapi.SampleInterval(time.Duration(path.Interval)*time.Second))
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
		go r.target.Subscribe(ctx, subReq, fmt.Sprintf("configserver %s", subEncoding.String()))
	}

	// stop the subscriptions once stopped
	defer r.target.StopSubscriptions()
	rspch, errCh := r.target.ReadSubscriptions()

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

			r.cache.Write(ctx, "", rsp.Response)
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
