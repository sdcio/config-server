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

// Accessors used by the metrics exporter. They run on the exporter's
// goroutine, concurrently with the target run loop.

func (t *TargetRuntime) Key() storebackend.Key { return t.key }

// Cache returns the telemetry cache of the current collector, or nil when no
// collector runs. The collector pointer is loaded exactly once: a concurrent
// stop can never turn it nil between the check and the use.
func (t *TargetRuntime) Cache() cache.Cache {
	if c := t.collector.Load(); c != nil {
		return c.cache
	}
	return nil
}

func (t *TargetRuntime) PromLabels() []prompb.Label {
	t.desiredMu.RLock()
	defer t.desiredMu.RUnlock()

	labels := make([]prompb.Label, 0, 5)
	labels = append(labels, prompb.Label{Name: "target", Value: t.key.Name})
	labels = append(labels, prompb.Label{Name: "namespace", Value: t.key.Namespace})

	if t.desired == nil {
		return labels
	}
	labels = append(labels,
		prompb.Label{Name: "vendor", Value: t.desired.Schema.Vendor},
		prompb.Label{Name: "version", Value: t.desired.Schema.Version},
		prompb.Label{Name: "address", Value: t.desired.Target.Address},
	)
	return labels
}
