package static


/*
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

	drclient := &target.DRClient{
		Client: r.client,
		DR:     dr.DiscoveryRule,
	}

	existingTargets, err := drclient.ListTargets(ctx)
	if err != nil {
		return err
	}

	for _, t := range r.drrule.Spec.Targets {
		t := t
		di := &invv1alpha1.DiscoveryInfo{
			Type:     discoverer.GetType(),
			Vendor:   discoverer.GetVendor(),
			Version:  r.drrule.Spec.Schema.Version,
			HostName: t.HostName,
		}
		// For static Discovery rules we pick the first profile in the list
		// if no profiles are available we return an error
		if len(dr.Profiles) == 0 || len(dr.DiscoveryRule.Spec.Profiles) == 0 {
			return fmt.Errorf("cannot create a target without a connection profile")
		}

		newTargetCR, err := drclient.NewTargetCR(
			ctx,
			fmt.Sprintf("%s:%d", t.IP, *&dr.Profiles[0].ConnectionProfile.Spec.Port),
			&dr.DiscoveryRule.Spec.Profiles[0],
			di,
			nil,
			discoverer.GetName(),
		)
		if err != nil {
			return err
		}

		if err := drclient.ApplyTarget(ctx, newTargetCR, di); err != nil {
			return err
		}
		// delete the target from the existing targets
		delete(existingTargets, types.NamespacedName{Namespace: newTargetCR.Namespace, Name: newTargetCR.Name})
	}

	for _, targetCR := range existingTargets {
		drclient.Delete(ctx, targetCR)
	}
	return nil
}
*/