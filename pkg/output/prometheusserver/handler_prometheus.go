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

package prometheusserver

import (
	"context"
	"fmt"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/openconfig/gnmic/pkg/api/path"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sdcio/config-server/pkg/target"
)

// Describe implements prometheus.Collector
func (r *PrometheusServer) Describe(ch chan<- *prometheus.Desc) {}

func (r *PrometheusServer) Collect(ch chan<- prometheus.Metric) {
	ctx := context.Background()

	keys := []storebackend.Key{}
	r.targetStore.List(ctx, func(ctx1 context.Context, k storebackend.Key, tctx *target.Context) {
		keys = append(keys, k)
	})

	for _, key := range keys {
		log := log.FromContext(ctx).With("target", key.String())
		tctx, err := r.targetStore.Get(ctx, key)
		if err != nil || tctx == nil {
			continue
		}
		cache := tctx.GetCache()
		if cache == nil {
			continue
		}
		notifications, err := cache.ReadAll()
		if err != nil {
			log.Error("cannot read from cache", "err", err)
			continue
		}
		for subName, notifs := range notifications {
			for _, notif := range notifs {
				for _, update := range notif.GetUpdate() {
					log.Info("prometheus collect", "update", update)

					// TODO user input
					promMetric, err := NewPromMetric(subName, tctx, update)
					if err != nil {
						fmt.Println("error getting prom metric",
							path.GnmiPathToXPath(update.GetPath(), false),
							update.GetVal(),
							err.Error(),
						)
						continue
					}
					log.Info("prometheus collect", "name", promMetric.Name, "data", promMetric.String())
					ch <- promMetric
				}
			}
		}
	}
}
