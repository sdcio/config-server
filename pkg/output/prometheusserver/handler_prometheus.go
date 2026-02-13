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

	"github.com/henderiw/logger/log"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/gnmic/pkg/api/path"
	"github.com/prometheus/client_golang/prometheus"
	targetruntimeview "github.com/sdcio/config-server/pkg/sdc/target/runtimeview"
)

// Describe implements prometheus.Collector
func (r *PrometheusServer) Describe(ch chan<- *prometheus.Desc) {}

func (r *PrometheusServer) Collect(ch chan<- prometheus.Metric) {
	ctx := context.Background()
	log := log.FromContext(ctx)

	runtimes := make([]targetruntimeview.TargetRuntimeView, 0)
    r.targetManager.ForEachRuntime(func(rt targetruntimeview.TargetRuntimeView) {
        runtimes = append(runtimes, rt)
    })
	log.Info("prometheus collect", "targets", len(runtimes))

	wg1 := new(sync.WaitGroup)
	wg1.Add(len(runtimes))
	for _, rt := range runtimes {
		rt := rt
		go func() {
			defer wg1.Done()
			log := log.With("target", rt.Key().String())

			cache := rt.Cache()
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
									promMetric, err := NewPromMetric(subName, rt, update)
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
		}()
	}
	wg1.Wait()
}
