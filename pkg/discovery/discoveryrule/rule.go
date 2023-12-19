package discoveryrule

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/henderiw/logger/log"
	"golang.org/x/sync/semaphore"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DiscoveryRule interface {
	Run(ctx context.Context) error
	Stop(ctx context.Context)
	GetDiscoveryRulConfig() *DiscoveryRuleConfig
}

func New(client client.Client, cfg *DiscoveryRuleConfig) DiscoveryRule {
	r := &dr{}
	r.client = client
	r.cfg = cfg
	r.protocols = r.newDiscoveryProtocols()
	return r
}

type dr struct {
	client    client.Client
	cfg       *DiscoveryRuleConfig
	protocols *protocols

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
				time.Sleep(r.cfg.CR.GetDiscoveryParameters().Period.Duration)
			}
		}
	}
}

func (r *dr) run(ctx context.Context) error {
	log := log.FromContext(ctx)
	hi, err := r.getHosts(ctx)
	if err != nil {
		return err // unlikely since the hosts/prefixes were validated before
	}
	t, err := r.getTargets(ctx)
	if err != nil {
		return err // happens only of the apiserver is unresponsive
	}

	sem := semaphore.NewWeighted(r.cfg.CR.GetDiscoveryParameters().ConcurrentScans)
	for {
		// Blocks until a next resource comes available
		err = sem.Acquire(ctx, 1)
		if err != nil {
			return err
		}
		h, ok := hi.Next()
		if !ok {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			go func(h *hostInfo, targets *targets) {
				log := log.With("ip", h.Addr.String())
				defer sem.Release(1)
				if !r.cfg.Discovery {
					log.Info("disovery disabled")
					// No discovery this is a static target
					if err := r.applyStaticTarget(ctx, h, targets); err != nil {
						// TODO reapply if update failed
						if strings.Contains(err.Error(), "the object has been modified; please apply your changes to the latest version") {
							// we will rety once
							if err := r.applyStaticTarget(ctx, h, targets); err != nil {
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
				if err := r.discover(ctx, h, targets); err != nil {
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
			t.del(h.Addr.String())
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
