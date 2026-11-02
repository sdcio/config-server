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

package keyring

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	resultOK         = "ok"
	resultReadError  = "read_error"
	resultParseError = "parse_error"
)

var (
	// reloadTotal makes a silently-failing reload visible. A keyring that stops
	// updating because the Secret is malformed looks identical to one that was
	// never rotated, unless this counter is watched.
	//
	// Alert on: increase(sdc_keyring_reload_total{result!="ok"}[15m]) > 0
	reloadTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "sdc_keyring_reload_total",
		Help: "Keyring reload attempts by outcome.",
	}, []string{"result"})

	// primaryInfo is 1 for the current primary key ID and absent for all others.
	// Across replicas this exposes the propagation window during rotation:
	// count(count by (keyid) (sdc_keyring_primary)) > 1 means replicas disagree.
	primaryInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "sdc_keyring_primary",
		Help: "Current primary key ID (1 for the primary).",
	}, []string{"keyid"})

	// keysLoaded is the size of the ring. It should return to 1 once a rotation
	// has fully drained and the retired key has been removed.
	keysLoaded = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "sdc_keyring_keys_loaded",
		Help: "Number of keys currently held in the keyring.",
	})
)

func init() {
	ctrlmetrics.Registry.MustRegister(reloadTotal, primaryInfo, keysLoaded)
}

func recordReload(result string) {
	reloadTotal.WithLabelValues(result).Inc()
}

// recordKeyRing publishes the current ring state. Reset() drops the stale
// primary series so exactly one keyid is ever reported.
func recordKeyRing(kr *KeyRing) {
	ids := kr.KeyIDs()
	keysLoaded.Set(float64(len(ids)))

	primaryInfo.Reset()
	primaryInfo.WithLabelValues(kr.PrimaryID()).Set(1)
}

// RecordInitial publishes ring state at startup, before the watcher runs, so a
// replica that never rotates still exports its primary key ID.
func RecordInitial(kr *KeyRing) {
	recordKeyRing(kr)
}