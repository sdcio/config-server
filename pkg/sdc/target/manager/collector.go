/*
Copyright 2026 Nokia.

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

package targetmanager

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
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/protobuf/encoding/prototext"
)

type Collector struct {
	targetKey     storebackend.Key
	subChan       chan struct{}
	subscriptions *Subscriptions
	cache         cache.Cache

	m      sync.RWMutex
	target *target.Target
	port   uint
	paths  map[invv1alpha1.Encoding][]Path

	// collector lifetime
	cancel context.CancelFunc

	// subscription lifetime (ONLY for curr
	subscriptionCancel context.CancelFunc
}

func NewCollector(targetKey storebackend.Key, subscriptions *Subscriptions) *Collector {
	cache, _ := cache.New(&cache.Config{Type: cache.CacheType("oc")})
	return &Collector{
		targetKey:     targetKey,
		subscriptions: subscriptions,
		subChan:       make(chan struct{}),
		cache:         cache,
	}
}

func (r *Collector) NotifySubscriptionChanged() {
	select {
	case r.subChan <- struct{}{}:
	default:
	}
}

func (r *Collector) SetPort(port uint) {
	r.m.Lock()
	defer r.m.Unlock()
	r.port = port
}

func (r *Collector) GetPort() uint {
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

	// stop subscription loop
	r.stopSubscriptionLocked()

	// stop collector loop
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}

	// close target
	if r.target != nil {
		r.target.StopSubscriptions()
		if err := r.target.Close(); err != nil {
			log.Error("close error", "err", err)
		}
		r.target = nil
	}
	r.paths = nil
}

func (r *Collector) Start(ctx context.Context, req *sdcpb.CreateDataStoreRequest) error {
	// stop existing (safe)
	r.Stop(ctx)

	r.m.Lock()
	// start collector lifetime
	runCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel

	log := log.FromContext(ctx).With("name", "targetCollector", "target", r.targetKey.String())

	// build target (needs port)
	tOpts := []gapi.TargetOption{
		gapi.Name(r.targetKey.String()),
		gapi.Address(fmt.Sprintf("%s:%d", req.Target.Address, req.Target.Port)),
		gapi.Username(string(req.Target.Credentials.Username)),
		gapi.Password(string(req.Target.Credentials.Password)),
		gapi.Timeout(5 * time.Second),
	}
	if req.Target.Tls == nil {
		tOpts = append(tOpts, gapi.Insecure(true))
	} else {
		tOpts = append(tOpts, gapi.SkipVerify(req.Target.Tls.SkipVerify))
		tOpts = append(tOpts, gapi.TLSCA(req.Target.Tls.Ca))
		tOpts = append(tOpts, gapi.TLSCert(req.Target.Tls.Cert))
		tOpts = append(tOpts, gapi.TLSKey(req.Target.Tls.Key))
	}

	var err error
	r.target, err = gapi.NewTarget(tOpts...)
	if err != nil {
		r.cancel = nil
		r.m.Unlock()
		log.Error("cannot create gnmi target", "err", err)
		return err
	}
	if err := r.target.CreateGNMIClient(runCtx); err != nil {
		_ = r.target.Close()
		r.target = nil
		r.cancel = nil
		r.m.Unlock()
		log.Error("cannot create gnmi client", "err", err)
		return err
	}

	// reset path snapshot so first update triggers
	r.paths = nil
	r.m.Unlock()

	go r.start(runCtx)
	return nil
}

func (r *Collector) start(ctx context.Context) {
	log := log.FromContext(ctx).With("name", "targetCollector", "target", r.targetKey.String())
	log.Info("start collector")

	// initial apply
	r.update(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.subChan:
			r.update(ctx)
		}
	}
}

func (r *Collector) update(ctx context.Context) {
	r.m.Lock()
	defer r.m.Unlock()

	log := log.FromContext(ctx).With("name", "targetCollector", "target", r.targetKey.String())

	if r.subscriptions == nil {
		log.Debug("no subscriptions")
		r.stopSubscriptionLocked()
		r.paths = nil
		return
	}

	newPaths := r.subscriptions.GetPaths()
	log.Debug("subscription update received", "newPaths", newPaths)

	if !r.hasPathsChanged(newPaths) {
		log.Debug("subscription did not change")
		return
	}
	log.Debug("subscription changed", "newPaths", newPaths, "existingPaths", r.paths)

	// stop current subscription loop
	r.stopSubscriptionLocked()

	// apply new snapshot
	r.paths = newPaths

	// if no paths => nothing to run
	if len(newPaths) == 0 {
		log.Info("no subscription paths left; subscription loop stopped")
		return
	}

	// start new subscription loop with its own cancel
	subCtx, cancel := context.WithCancel(ctx)
	r.subscriptionCancel = cancel
	go r.startSubscription(subCtx, newPaths)
}

func (r *Collector) stopSubscriptionLocked() {
	// IMPORTANT: caller holds r.m.Lock()
	if r.subscriptionCancel != nil {
		r.subscriptionCancel()
		r.subscriptionCancel = nil
	}
	if r.target != nil {
		r.target.StopSubscriptions()
	}
}

func (r *Collector) hasPathsChanged(newPaths map[invv1alpha1.Encoding][]Path) bool {
	// IMPORTANT: caller holds r.m.Lock()
	old := r.paths
	if old == nil && len(newPaths) == 0 {
		return false
	}
	if len(old) != len(newPaths) {
		return true
	}
	for enc, np := range newPaths {
		op, ok := old[enc]
		if !ok {
			return true
		}
		if len(op) != len(np) {
			return true
		}
		// If order is not guaranteed, you should sort; see section 4 below.
		for i := range op {
			if op[i].Path != np[i].Path || op[i].Interval != np[i].Interval {
				return true
			}
		}
	}
	return false
}

func (r *Collector) StopSubscription(ctx context.Context) {
	log := log.FromContext(ctx).With("name", "targetCollector", "target", r.targetKey.String())
	log.Info("stop subscription")

	r.m.Lock()
	defer r.m.Unlock()

	r.paths = nil
	r.stopSubscriptionLocked()
}

func (r *Collector) startSubscription(ctx context.Context, paths map[invv1alpha1.Encoding][]Path) {
	log := log.FromContext(ctx).With("target", r.targetKey.String())
	log.Info("starting subscription collector", "paths", paths)

START:
	// subscribe using received paths
	for subEncoding, paths := range paths {
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
			log.Info("subscription collector stopped")
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
