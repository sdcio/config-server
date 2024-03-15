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

	"github.com/henderiw/logger/log"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (r *dr) discoverWithNone(ctx context.Context, h *hostInfo, connProfile *invv1alpha1.TargetConnectionProfile) error {
	log := log.FromContext(ctx)
	log.Info("discover protocol none", "hostName", h.hostName)
	provider := r.cfg.DefaultSchema.Provider
	version := r.cfg.DefaultSchema.Version
	address := fmt.Sprintf("%s:%d",
		h.Address,
		r.cfg.TargetConnectionProfiles[0].Connectionprofile.Spec.Port,
	)
	di := &invv1alpha1.DiscoveryInfo{
		Protocol: "static",
		Provider: provider,
		Version:  version,
		HostName: h.hostName,
		LastSeen: metav1.Now(),
	}
	return r.createTarget(ctx, provider, address, di)
}
