package discoveryrule

import (
	"context"
	"errors"
	"fmt"

	"github.com/henderiw/logger/log"
	"github.com/iptecharch/config-server/apis/inv/v1alpha1"
	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
)

func (r *dr) discover(ctx context.Context, h *hostInfo, targets *targets) error {
	// this is a dynamic target; was it alreaady discovered?
	// if discovery was already done use the same initial discovery protocol
	address := h.Address
	log := log.FromContext(ctx).With("address", address)

	var err error
	for _, connProfile := range r.getDiscoveryProfiles(ctx, h, targets) {
		discover, ok := r.protocols.get(connProfile.Spec.Protocol)
		if !ok {
			log.Info("connection profile points to unsupported protocol", "protocol", connProfile.Spec.Protocol)
			err = errors.Join(err, fmt.Errorf("unsupported protocol :%s", string(connProfile.Spec.Protocol)))
			continue
		}
		if derr := discover(ctx, address, connProfile); derr != nil {
			log.Info("discovery failed", "protocol", connProfile.Spec.Protocol)
			err = errors.Join(err, derr)
			continue
		}
		// discovery succeeded
		return nil
	}
	// we return the aggregated error
	return err

}

// retruns the profiles used for discovery; if discovery was already done we retry with the same profile first
// this function returns the discovery connection profile list and the order is changed based on the fact discovery
// was already done
func (r *dr) getDiscoveryProfiles(ctx context.Context, h *hostInfo, targets *targets) []*v1alpha1.TargetConnectionProfile {
	address := h.Address

	found := false // represent the status of the fact that we found the initial discovery profile
	discoveryProfiles := make([]*invv1alpha1.TargetConnectionProfile, 0, len(r.cfg.DiscoveryProfile.Connectionprofiles))
	t, ok := targets.get(address)
	if ok {
		// target exsists
		if t.Status.DiscoveryInfo != nil { // safety
			for _, connProfile := range r.cfg.DiscoveryProfile.Connectionprofiles {
				if string(connProfile.Spec.Protocol) == t.Status.DiscoveryInfo.Protocol {
					// initial discovery profile found
					found = true
					discoveryProfiles = append(discoveryProfiles, connProfile)
				}
			}
		}
	}
	for _, connProfile := range r.cfg.DiscoveryProfile.Connectionprofiles {
		if found {
			if connProfile.GetName() != discoveryProfiles[0].GetName() {
				discoveryProfiles = append(discoveryProfiles, connProfile)
			}
		} else {
			discoveryProfiles = append(discoveryProfiles, connProfile)
		}
	}
	return discoveryProfiles
}
