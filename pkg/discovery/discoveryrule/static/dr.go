package static

import (
	"context"
	"fmt"
	"time"

	"github.com/henderiw/logger/log"
	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/discovery/discoverers"
	"github.com/iptecharch/config-server/pkg/discovery/discoveryrule"
	"github.com/iptecharch/config-server/pkg/discovery/discoveryrule/target"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func init() {
	discoveryrule.Register(schema.GroupVersionKind{
		Group:   invv1alpha1.SchemeGroupVersion.Group,
		Version: invv1alpha1.SchemeGroupVersion.Version,
		Kind:    invv1alpha1.DiscoveryRuleStaticKind,
	}, func(client client.Client) discoveryrule.DiscoveryRule {
		return &static{
			client: client,
		}
	})
}

type static struct {
	client client.Client
	cancel context.CancelFunc
	drrule *invv1alpha1.DiscoveryRuleStatic
}

func (r *static) Get(ctx context.Context, key types.NamespacedName) (string, error) {
	dr := &invv1alpha1.DiscoveryRuleStatic{}
	if err := r.client.Get(ctx, key, dr); err != nil {
		return "", err
	}
	r.drrule = dr

	return dr.ResourceVersion, nil
}

func (r *static) Run(ctx context.Context, dr *invv1alpha1.DiscoveryRuleContext) error {
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
			// TBD: we could stop here, since the data
			time.Sleep(dr.DiscoveryRule.Spec.Period.Duration)
		}
	}
}

func (r *static) Stop(ctx context.Context) {
	if r.cancel != nil {
		r.cancel()
	}
}

func (r *static) run(ctx context.Context, dr *invv1alpha1.DiscoveryRuleContext) error {
	log := log.FromContext(ctx).With("drName", dr.DiscoveryRule.GetName())
	log.Info("run static drRule")
	init, exists := discoverers.Discoverers[r.drrule.Spec.Schema.Provider]
	if !exists {
		return fmt.Errorf("provider %s not registered", r.drrule.Spec.Schema.Provider)
	}
	discoverer := init()

	for _, t := range r.drrule.Spec.Targets {
		di := &invv1alpha1.DiscoveryInfo{
			Type:     discoverer.GetType(),
			Vendor:   discoverer.GetVendor(),
			Version:  r.drrule.Spec.Schema.Version,
			HostName: t.HostName,
		}

		if err := target.ApplyTarget(ctx, r.client, dr, di, t.Address, nil, discoverer.GetName()); err != nil {
			return err
		}
	}
	return nil
}
