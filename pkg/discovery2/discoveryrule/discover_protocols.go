package discoveryrule

import (
	"context"
	"sync"

	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
)

func (r *dr) newDiscoveryProtocols() *protocols {
	return &protocols{
		protocols: map[invv1alpha1.Protocol]discover{
			invv1alpha1.Protocol_GNMI: r.discoverWithGNMI,
		},
	}
}

type discover func(ctx context.Context, ip string, connProfile *invv1alpha1.TargetConnectionProfile) error

type protocols struct {
	m         sync.RWMutex
	protocols map[invv1alpha1.Protocol]discover
}

func (r *protocols) get(p invv1alpha1.Protocol) (discover, bool) {
	r.m.RLock()
	defer r.m.RUnlock()
	d, ok := r.protocols[p]
	return d, ok
}
