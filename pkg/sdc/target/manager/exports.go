/*
Copyright 2026 Nokia.

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

package targetmanager

import (
	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/openconfig/gnmic/pkg/cache"
	"github.com/prometheus/prometheus/prompb"
)

func (t *TargetRuntime) Key() storebackend.Key { return t.key }

func (t *TargetRuntime) Cache() cache.Cache {
	t.runningMu.RLock()
	defer t.runningMu.RUnlock()
	if t.collector == nil {
		return nil
	}
	return t.collector.cache
}

func (t *TargetRuntime) PromLabels() []prompb.Label {
	t.desiredMu.RLock()
	defer t.desiredMu.RUnlock()

	labels := make([]prompb.Label, 0, 4)
	labels = append(labels, prompb.Label{Name: "target", Value: t.key.Name})
	labels = append(labels, prompb.Label{Name: "namespace", Value: t.key.Namespace})

	if t.desired != nil && t.desired.Schema != nil {
		labels = append(labels,
			prompb.Label{Name: "vendor", Value: t.desired.Schema.Vendor},
			prompb.Label{Name: "version", Value: t.desired.Schema.Version},
			prompb.Label{Name: "address", Value: t.desired.Target.Address},
		)
	}
	return labels
}

