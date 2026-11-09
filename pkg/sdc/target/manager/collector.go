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
	"github.com/openconfig/gnmi/proto/gnmi"
	gapi "github.com/openconfig/gnmic/pkg/api"
	"github.com/openconfig/gnmic/pkg/api/target"
	"github.com/openconfig/gnmic/pkg/cache"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	logf "github.com/sdcio/logger"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/protobuf/encoding/prototext"
)

const subscriptionRetryBackoff = 5 * time.Second

type Collector struct {
	targetKey     storebackend.Key
	subChan       chan struct{} // buffered(1): coalescing, never loses a notification
	subscriptions *Subscriptions
	cache         cache.Cache

	m       sync.RWMutex
	target  *target.Target
	address string // host:port the gNMI client was created for
	paths   map[invv1alpha1.Encoding][]Path

	// collector lifetime
	cancel context.CancelFunc

	// subscription lifetime (only for the current path snapshot)
	subscriptionCancel context.CancelFunc
}

func NewCollector(targetKey storebackend.Key, subscriptions *Subscriptions) *Collector {
	cache, _ := cache.New(&cache.Config{Type: cache.CacheType("oc")})
	return &Collector{
		targetKey:     targetKey,
		subscriptions: subscriptions,
		subChan:       make(chan struct{}, 1),
		cache:         cache,
	}
}

func (r *Collector) NotifySubscriptionChanged() {
	select {
	case r.subChan <- struct{}{}:
	default:
	}
}

// Address returns the endpoint the running collector is connected to ("" when stopped).
func (r *Collector) Address() string {
	r.m.RLock()
	defer r.m.RUnlock()
	return r.address
}

func (r *Collector) IsRunning() bool {
	r.m.RLock()
	defer r.m.RUnlock()
	return r.cancel != nil
}

func (r *Collector) Stop(ctx context.Context) {
	r.m.Lock()
	defer r.m.Unlock()

	log := logf.FromContext(ctx).WithValues("name", "targetCollector", "target", r.targetKey.String())
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
			log.Error(err, "close error")
		}
		r.target = nil
	}
	r.address = ""
	r.paths = nil
}

// Start (re)connects the collector to address using the target's credentials
// and TLS settings. Eligibility (gnmi, port, active paths) is decided by the
// runtime; the checks here are only defensive.
func (r *Collector) Start(ctx context.Context, req *sdcpb.CreateDataStoreRequest, address string) error {
	if req == nil || req.Target == nil {
		return fmt.Errorf("collector: no target")
	}
	if address == "" {
		return fmt.Errorf("collector: no address")
	}

	// stop existing (safe)
	r.Stop(ctx)

	log := logf.FromContext(ctx).WithValues("name", "targetCollector", "target", r.targetKey.String())

	tOpts := []gapi.TargetOption{
		gapi.Name(r.targetKey.String()),
		gapi.Address(address),
		gapi.Timeout(5 * time.Second),
	}
	if c := req.Target.Credentials; c != nil {
		tOpts = append(tOpts,
			gapi.Username(string(c.Username)),
			gapi.Password(string(c.Password)),
		)
	}
	if req.Target.Tls == nil {
		tOpts = append(tOpts, gapi.Insecure(true))
	} else {
		tOpts = append(tOpts,
			gapi.SkipVerify(req.Target.Tls.SkipVerify),
			gapi.TLSCA(req.Target.Tls.Ca),
			gapi.TLSCert(req.Target.Tls.Cert),
			gapi.TLSKey(req.Target.Tls.Key),
		)
	}

	tg, err := gapi.NewTarget(tOpts...)
	if err != nil {
		return fmt.Errorf("create gnmi target %s: %w", address, err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	// dial outside the lock
	if err := tg.CreateGNMIClient(runCtx); err != nil {
		cancel()
		_ = tg.Close()
		return fmt.Errorf("create gnmi client %s: %w", address, err)
	}

	r.m.Lock()
	r.target = tg
	r.address = address
	r.cancel = cancel
	r.paths = nil // reset path snapshot so first update triggers
	r.m.Unlock()

	log.Info("collector started", "address", address)
	go r.start(runCtx)
	return nil
}

func (r *Collector) start(ctx context.Context) {
	log := logf.FromContext(ctx).WithValues("name", "targetCollector", "target", r.targetKey.String())
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

	log := logf.FromContext(ctx).WithValues("name", "targetCollector", "target", r.targetKey.String())

	// Stop() cancels ctx while holding r.m, so this reliably detects a stale
	// loop from a previous Start and keeps it off the new target's state.
	if ctx.Err() != nil || r.target == nil {
		return
	}

	if r.subscriptions == nil {
		log.V(logf.VDebug).Info("no subscriptions")
		r.stopSubscriptionLocked()
		r.paths = nil
		return
	}

	newPaths := r.subscriptions.GetPaths()
	log.V(logf.VDebug).Info("subscription update received", "newPaths", newPaths)

	if !r.hasPathsChanged(newPaths) {
		log.V(logf.VDebug).Info("subscription did not change")
		return
	}
	log.V(logf.VDebug).Info("subscription changed", "newPaths", newPaths, "existingPaths", r.paths)

	// stop current subscription loop
	r.stopSubscriptionLocked()

	// apply new snapshot
	r.paths = newPaths

	// if no paths => nothing to run
	if len(newPaths) == 0 {
		log.Info("no subscription paths left; subscription loop stopped")
		return
	}

	// start new subscription loop with its own cancel; pass the target
	// explicitly so the loop never dereferences r.target after Stop().
	subCtx, cancel := context.WithCancel(ctx)
	r.subscriptionCancel = cancel
	go r.runSubscription(subCtx, r.target, newPaths)
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
	// GetPaths returns paths sorted per encoding, so index comparison is safe.
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
		for i := range op {
			if op[i].Path != np[i].Path || op[i].Interval != np[i].Interval {
				return true
			}
		}
	}
	return false
}

func (r *Collector) StopSubscription(ctx context.Context) {
	log := logf.FromContext(ctx).WithValues("name", "targetCollector", "target", r.targetKey.String())
	log.Info("stop subscription")

	r.m.Lock()
	defer r.m.Unlock()

	r.paths = nil
	r.stopSubscriptionLocked()
}

// runSubscription keeps the subscriptions for one path snapshot alive until
// ctx is cancelled, restarting the streams with a backoff on failure.
func (r *Collector) runSubscription(ctx context.Context, tg *target.Target, paths map[invv1alpha1.Encoding][]Path) {
	log := logf.FromContext(ctx).WithValues("target", r.targetKey.String())
	log.Info("starting subscription collector", "paths", paths)

	reqs, err := buildSubscribeRequests(paths)
	if err != nil {
		// deterministic for a given snapshot: retrying would not help
		log.Error(err, "cannot build subscribe request")
		return
	}

	defer tg.StopSubscriptions()
	for {
		r.stream(ctx, tg, reqs)
		tg.StopSubscriptions()

		select {
		case <-ctx.Done():
			log.Info("subscription collector stopped")
			return
		case <-time.After(subscriptionRetryBackoff):
			log.Info("restarting subscriptions")
		}
	}
}

// stream runs the subscriptions until ctx is done or a stream fails.
func (r *Collector) stream(ctx context.Context, tg *target.Target, reqs map[string]*gnmi.SubscribeRequest) {
	log := logf.FromContext(ctx).WithValues("target", r.targetKey.String())

	for name, req := range reqs {
		log.Info("subscription request", "name", name, "req", prototext.Format(req))
		go tg.Subscribe(ctx, req, name)
	}

	rspCh, errCh := tg.ReadSubscriptions()
	for {
		select {
		case <-ctx.Done():
			return
		case rsp, ok := <-rspCh:
			if !ok {
				return
			}
			r.handleResponse(ctx, rsp)
		case tErr, ok := <-errCh:
			if !ok {
				return
			}
			if tErr != nil && tErr.Err != nil {
				log.Error(tErr.Err, "subscription failed")
				return
			}
		}
	}
}

func (r *Collector) handleResponse(ctx context.Context, rsp *target.SubscribeResponse) {
	if rsp == nil || rsp.Response == nil {
		return
	}
	logf.FromContext(ctx).V(logf.VDebug).Info("subscription update", "update", rsp.Response)

	switch resp := rsp.Response.ProtoReflect().Interface().(type) {
	case *gnmi.SubscribeResponse:
		if u, ok := resp.GetResponse().(*gnmi.SubscribeResponse_Update); ok {
			if u.Update.GetPrefix() == nil {
				u.Update.Prefix = new(gnmi.Path)
			}
			if u.Update.GetPrefix().GetTarget() == "" {
				u.Update.Prefix.Target = r.targetKey.String()
			}
		}
	}
	r.cache.Write(ctx, "", rsp.Response)
}

func buildSubscribeRequests(paths map[invv1alpha1.Encoding][]Path) (map[string]*gnmi.SubscribeRequest, error) {
	reqs := make(map[string]*gnmi.SubscribeRequest, len(paths))
	for enc, encPaths := range paths {
		opts := make([]gapi.GNMIOption, 0, len(encPaths)+2)
		for _, p := range encPaths {
			subOpts := []gapi.GNMIOption{gapi.Path(p.Path)}
			if p.Interval == 0 {
				subOpts = append(subOpts, gapi.SubscriptionModeON_CHANGE())
			} else {
				subOpts = append(subOpts,
					gapi.SubscriptionModeSAMPLE(),
					gapi.SampleInterval(time.Duration(p.Interval)*time.Second),
				)
			}
			opts = append(opts, gapi.Subscription(subOpts...))
		}
		opts = append(opts,
			gapi.EncodingCustom(encoding(enc.String())),
			gapi.SubscriptionListModeSTREAM(),
		)
		req, err := gapi.NewSubscribeRequest(opts...)
		if err != nil {
			return nil, fmt.Errorf("encoding %s: %w", enc.String(), err)
		}
		reqs[fmt.Sprintf("configserver %s", enc.String())] = req
	}
	return reqs, nil
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
