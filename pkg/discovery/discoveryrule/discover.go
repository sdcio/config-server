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
	"errors"
	"fmt"

	"github.com/henderiw/logger/log"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
)

func (r *dr) discover(ctx context.Context, h *hostInfo) error {
	// this is a dynamic target; was it alreaady discovered?
	// if discovery was already done use the same initial discovery protocol
	address := h.Address
	log := log.FromContext(ctx).With("address", address)
	if !r.cfg.Discovery {
		// discovery disabled
		if h.hostName == "" {
			return fmt.Errorf("cannot create a static target w/o a hostname")
		}
		if len(r.cfg.TargetConnectionProfiles) == 0 {
			return fmt.Errorf("cannot create a static target w/o a connectivity profile")
		}
		if r.cfg.DefaultSchema == nil {
			return fmt.Errorf("cannot create a static target w/o a default schema")
		}
		discover, ok := r.protocols.get(invv1alpha1.Protocol_NONE)
		if !ok {
			return fmt.Errorf("unsupported protocol :%s", string(invv1alpha1.Protocol_NONE))
		}
		if err := discover(ctx, h, r.cfg.TargetConnectionProfiles[0].Connectionprofile); err != nil {
			log.Error("discovery failed", "error", err.Error())
			return err
		}
		// discover w/o discovery succeeded
		return nil
	}
	// discovery enabled
	var err error
	for _, connProfile := range r.getDiscoveryProfiles(ctx, h) {
		discover, ok := r.protocols.get(connProfile.Spec.Protocol)
		if !ok {
			log.Info("connection profile points to unsupported protocol", "protocol", connProfile.Spec.Protocol)
			err = errors.Join(err, fmt.Errorf("unsupported protocol :%s", string(connProfile.Spec.Protocol)))
			continue
		}
		if derr := discover(ctx, h, connProfile); derr != nil {
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
func (r *dr) getDiscoveryProfiles(_ context.Context, _ *hostInfo) []*invv1alpha1.TargetConnectionProfile {
	//address := h.Address

	found := false // represent the status of the fact that we found the initial discovery profile
	discoveryProfiles := make([]*invv1alpha1.TargetConnectionProfile, 0, len(r.cfg.DiscoveryProfile.Connectionprofiles))
	// TODO optimize reuse of the exisiting profile -> if discovery is already done, do we reuse the profile that was successfull
	/*
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
	*/
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
