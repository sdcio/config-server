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
	"sync"

	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
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
