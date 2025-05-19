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
	"sync"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/openconfig/gnmi/proto/gnmi"
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
	log := log.FromContext(ctx)
	log.Info("prometheus collect")

	wg1 := new(sync.WaitGroup)

	nKeys := len(keys)
	targetMetricsAvailable := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "sdcio_target_metrics_available",
		Help: "1.0 means at least one target metric is available while 0.0 means no target metrics to export. Ensure subscriptions are declaired to export target metrics as documented on https://docs.sdcio.dev/user-guide/configuration/subscription/subscription/ .",
	})
	if nKeys <= 0 {
		targetMetricsAvailable.Set(0.0)
	} else {
		targetMetricsAvailable.Set(1.0)
	}
	ch <- targetMetricsAvailable

	wg1.Add(len(keys))
	for _, key := range keys {
		go func(key storebackend.Key) {
			defer wg1.Done()
			log := log.With("target", key.Name)
			tctx, err := r.targetStore.Get(ctx, key)
			if err != nil || tctx == nil {
				return
			}
			cache := tctx.GetCache()
			if cache == nil {
				return
			}
			notifications, err := cache.ReadAll()
			if err != nil {
				log.Error("cannot read from cache", "err", err)
				return
			}
			wg2 := new(sync.WaitGroup)
			wg2.Add(len(notifications))
			for subName, notifs := range notifications {
				go func(subName string) {
					defer wg2.Done()
					wg3 := new(sync.WaitGroup)
					wg3.Add(len(notifs))
					for _, notif := range notifs {
						go func(notif *gnmi.Notification) {
							defer wg3.Done()
							wg4 := new(sync.WaitGroup)
							wg4.Add(len(notif.GetUpdate()))
							for _, update := range notif.GetUpdate() {
								go func(update *gnmi.Update) {
									defer wg4.Done()
									// TODO user input
									promMetric, err := NewPromMetric(subName, tctx, update)
									if err != nil {
										log.Debug("error getting prom metric",
											"path", path.GnmiPathToXPath(update.GetPath(), false),
											"val", update.GetVal(),
											"err", err.Error(),
										)
										fmt.Println("error getting prom metric",
											path.GnmiPathToXPath(update.GetPath(), false),
											update.GetVal(),
											err.Error(),
										)
										return
									}
									//log.Info("prometheus collect", "name", promMetric.Name, "data", promMetric.String())
									ch <- promMetric
								}(update)
							}
							wg4.Wait()
						}(notif)
					}
					wg3.Wait()
				}(subName)
			}
			wg2.Wait()
		}(key)
	}
	wg1.Wait()
}
