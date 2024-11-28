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

package discoveryrule

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/apiserver-store/pkg/storebackend/memory"
	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/pkg/target"
	"golang.org/x/sync/semaphore"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DiscoveryRule interface {
	Run(ctx context.Context) error
	Stop(ctx context.Context)
	GetDiscoveryRulConfig() *DiscoveryRuleConfig
}

func New(client client.Client, cfg *DiscoveryRuleConfig, targetStore storebackend.Storer[*target.Context]) DiscoveryRule {
	r := &dr{}
	r.client = client
	r.cfg = cfg
	r.protocols = r.newDiscoveryProtocols()
	r.targetStore = targetStore
	return r
}

type dr struct {
	client      client.Client
	cfg         *DiscoveryRuleConfig
	protocols   *protocols
	targetStore storebackend.Storer[*target.Context]
	children    storebackend.Storer[string]

	cancel context.CancelFunc
}

func (r *dr) Stop(ctx context.Context) {
	if r.cancel != nil {
		r.cancel()
	}
}

func (r *dr) GetDiscoveryRulConfig() *DiscoveryRuleConfig {
	return r.cfg
}

// Run holds the global run context
func (r *dr) Run(ctx context.Context) error {
	ctx, r.cancel = context.WithCancel(ctx)
	log := log.FromContext(ctx).With("discovery-rule", fmt.Sprintf("%s.%s", r.cfg.CR.GetNamespace(), r.cfg.CR.GetName()))
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// run DR
			err := r.run(ctx)
			if err != nil {
				log.Info("failed to run discovery rule", "error", err)
				time.Sleep(5 * time.Second)
			} else {
				log.Info("discovery rule finished, waiting for next run")
				time.Sleep(r.cfg.CR.GetDiscoveryParameters().GetPeriod().Duration)
			}
		}
	}
}

func (r *dr) run(ctx context.Context) error {
	log := log.FromContext(ctx)
	iter, err := r.getHosts(ctx)
	if err != nil {
		return err // unlikely since the hosts/prefixes were validated before
	}

	var wg sync.WaitGroup
	// clear the children list
	r.children = memory.NewStore[string]()
	defer r.deleteUnWantedChildren(ctx) // delete unwanted children

	sem := semaphore.NewWeighted(r.cfg.CR.GetDiscoveryParameters().GetConcurrentScans())
	for {
		// Blocks until a next resource comes available
		if err := sem.Acquire(ctx, 1); err != nil {
			return err
		}
		h, ok := iter.Next()
		if !ok {
			//sem.Release(1)
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			wg.Add(1)
			go func(h *hostInfo) {
				log := log.With("address", h.Address)
				defer sem.Release(1)
				defer wg.Done()
				// discover irrespective if discovery is enabled or disabled
				if err := r.discover(ctx, h); err != nil {
					//if status.Code(err) == codes.Canceled {
					if strings.Contains(err.Error(), "context cancelled") {
						log.Info("discovery cancelled")
					} else {
						log.Info("discovery failed", "error", err)
					}
				}
			}(h)
		}
	}
	wg.Wait() // Wait for all goroutines to finish
	return nil
}
