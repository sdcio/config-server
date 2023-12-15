package iprange

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/henderiw/logger/log"
	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/discovery/discoveryrule"
	"github.com/iptecharch/config-server/pkg/discovery/discoveryrule/target"
	"github.com/iptecharch/config-server/pkg/discovery/gnmi"
	"golang.org/x/sync/semaphore"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func init() {
	discoveryrule.Register(schema.GroupVersionKind{
		Group:   invv1alpha1.SchemeGroupVersion.Group,
		Version: invv1alpha1.SchemeGroupVersion.Version,
		Kind:    invv1alpha1.DiscoveryRuleIPRangeKind,
	}, func(client client.Client) discoveryrule.DiscoveryRule {
		return &ipRangeDR{
			client: client,
		}
	})
}

type ipRangeDR struct {
	client client.Client
	cancel context.CancelFunc
	drrule *invv1alpha1.DiscoveryRuleIPRange
}

func (r *ipRangeDR) Get(ctx context.Context, key types.NamespacedName) (string, error) {
	dr := &invv1alpha1.DiscoveryRuleIPRange{}
	if err := r.client.Get(ctx, key, dr); err != nil {
		return "", err
	}
	r.drrule = dr

	return dr.ResourceVersion, nil
}

func (r *ipRangeDR) Run(ctx context.Context, dr *invv1alpha1.DiscoveryRuleContext) error {
	ctx, r.cancel = context.WithCancel(ctx)

	log := log.FromContext(ctx).With("discovery-rule", fmt.Sprintf("%s.%s", dr.DiscoveryRule.GetNamespace(), dr.DiscoveryRule.GetName()))
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// run DR
			err := r.run(ctx, dr)
			if err != nil {
				log.Info("failed to run discovery rule", "error", err)
				time.Sleep(5 * time.Second)
			}
			log.Info("discovery rule finished, waiting for next run")
			time.Sleep(dr.DiscoveryRule.Spec.Period.Duration)
		}
	}
}

func (r *ipRangeDR) Stop(ctx context.Context) {
	if r.cancel != nil {
		r.cancel()
	}
}

func (r *ipRangeDR) run(ctx context.Context, dr *invv1alpha1.DiscoveryRuleContext) error {
	log := log.FromContext(ctx)

	hosts, err := discoveryrule.GetHosts(r.drrule.Spec.CIDRs...)
	if err != nil {
		return err
	}
	for _, e := range r.drrule.Spec.Excludes {
		excludes, err := discoveryrule.GetHosts(e)
		if err != nil {
			return err
		}
		for h := range excludes {
			delete(hosts, h)
		}
	}

	sem := semaphore.NewWeighted(2)
	for _, ip := range discoveryrule.SortIPs(hosts) {
		err = sem.Acquire(ctx, 1)
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			go func(ip string) {
				defer sem.Release(1)
				if err := r.discover(ctx, dr, ip); err != nil {
					//if status.Code(err) == codes.Canceled {
					if strings.Contains(err.Error(), "context canceled") {
						log.Info("discovery cancelled", "IP", ip)
					} else {
						log.Info("discovery failed", "IP", ip, "error", err)
					}

					// TODO: update the status
					return
				}
			}(ip)
		}
	}
	return nil
}

func (r *ipRangeDR) discover(ctx context.Context, dr *invv1alpha1.DiscoveryRuleContext, ip string) error {
	log := log.FromContext(ctx)

	switch dr.ConnectionProfile.Spec.Protocol {
	case "snmp":
		return nil
	case "netconf":
		return nil
	default: // gnmi
		t, err := gnmi.CreateTarget(ctx, r.client, dr, ip)
		if err != nil {
			return err
		}
		log.Info("Creating gNMI client", "IP", t.Config.Name)
		err = t.CreateGNMIClient(ctx)
		if err != nil {
			return err
		}
		defer t.Close()
		capRsp, err := t.Capabilities(ctx)
		if err != nil {
			return err
		}
		discoverer, err := gnmi.GetDiscovererGNMI(capRsp)
		if err != nil {
			return err
		}
		di, err := discoverer.Discover(ctx, dr, t)
		if err != nil {
			return err
		}
		b, _ := json.Marshal(di)
		log.Info("discovery info", "info", string(b))

		return target.ApplyTarget(ctx, r.client, dr, di, t.Config.Address, nil, discoverer.GetName())
	}
}
