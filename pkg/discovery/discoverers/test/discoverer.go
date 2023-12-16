package test

import (
	"context"

	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/discovery/discoverers"
	"github.com/openconfig/gnmic/pkg/target"
)

const (
	ProviderName = "test.sdcio.dev"
	Vendor       = "Test"
	Type         = "test"
)

func init() {
	discoverers.Register(ProviderName, func() discoverers.Discoverer {
		return &discoverer{}
	})
}

type discoverer struct{}

func (s *discoverer) GetName() string {
	return ProviderName
}

func (s *discoverer) GetType() string {
	return Type
}

func (s *discoverer) GetVendor() string {
	return Vendor
}

func (s *discoverer) Discover(ctx context.Context, dr *invv1alpha1.DiscoveryRuleContext, t *target.Target) (*invv1alpha1.DiscoveryInfo, error) {
	return &invv1alpha1.DiscoveryInfo{}, nil
}
