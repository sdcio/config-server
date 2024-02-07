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
	"time"

	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/pkg/lease"
	"github.com/sdcio/config-server/pkg/store"
	"github.com/sdcio/config-server/pkg/target"
	"golang.org/x/sync/semaphore"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DiscoveryRule interface {
	Run(ctx context.Context) error
	Stop(ctx context.Context)
	GetDiscoveryRulConfig() *DiscoveryRuleConfig
}

func New(client client.Client, cfg *DiscoveryRuleConfig, targetStore store.Storer[target.Context]) DiscoveryRule {
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
	targetStore store.Storer[target.Context]

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
	// targets 
	t, err := r.getTargets(ctx)
	if err != nil {
		return err // happens only of the apiserver is unresponsive
	}

	sem := semaphore.NewWeighted(r.cfg.CR.GetDiscoveryParameters().GetConcurrentScans())
	for {
		// Blocks until a next resource comes available
		err = sem.Acquire(ctx, 1)
		if err != nil {
			return err
		}
		h, ok := iter.Next()
		if !ok {
			sem.Release(1)
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			go func(h *hostInfo, targets *targets) {
				log := log.With("address", h.Address)
				defer sem.Release(1)
				if !r.cfg.Discovery {
					log.Info("disovery disabled")

					lease := r.getLease(ctx, store.KeyFromNSN(types.NamespacedName{
						Namespace: r.cfg.CR.GetNamespace(),
						Name:      getTargetName(h.hostName),
					}))

					if err := lease.AcquireLease(ctx, "DiscoveryController"); err != nil {
						log.Info("cannot acquire lease", "target", getTargetName(h.hostName), "error", err.Error())
						return
					}
					// No discovery this is a static target
					if err := r.applyStaticTarget(ctx, h); err != nil {
						// TODO reapply if update failed
						if strings.Contains(err.Error(), "the object has been modified; please apply your changes to the latest version") {
							// we will rety once, sometimes we get an error
							if err := r.applyStaticTarget(ctx, h); err != nil {
								log.Info("static target creation retry failed", "error", err)
							}
						} else {
							log.Info("static target creation failed", "error", err)
						}
					}
					return
				}
				log.Info("disovery enabled")
				// Discovery
				if err := r.discover(ctx, h); err != nil {
					//if status.Code(err) == codes.Canceled {
					if strings.Contains(err.Error(), "context canceled") {
						log.Info("discovery cancelled")
					} else {
						log.Info("discovery failed", "error", err)
					}
					// TBD update status
				}
			}(h, t)
			// delete the target since we processed it
			t.del(h.Address)
		}
	}
	// any target that was not processed we can delete as the ip rules dont cover this any longer
	for _, t := range t.list() {
		if err := r.client.Delete(ctx, t); err != nil {
			log.Error("cannot delete target")
		}
	}

	return nil
}

func (r *dr) getLease(ctx context.Context, targetKey store.Key) lease.Lease {
	tctx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil {
		lease := lease.New(r.client, targetKey.NamespacedName)
		r.targetStore.Create(ctx, targetKey, target.Context{Lease: lease})
		return lease
	}
	if tctx.Lease == nil {
		lease := lease.New(r.client, targetKey.NamespacedName)
		tctx.Lease = lease
		r.targetStore.Update(ctx, targetKey, target.Context{Lease: lease})
		return lease
	}
	return tctx.Lease
}
