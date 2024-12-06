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
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/gnmic/pkg/path"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sdcio/config-server/pkg/target"
)

const (
	metricNameRegex = "[^a-zA-Z0-9_]+"
)

// Describe implements prometheus.Collector
func (r *PrometheusServer) Describe(ch chan<- *prometheus.Desc) {}

func (r *PrometheusServer) Collect(ch chan<- prometheus.Metric) {
	ctx := context.Background()

	keys := []storebackend.Key{}
	r.targetStore.List(ctx, func(ctx1 context.Context, k storebackend.Key, tctx *target.Context) {
		keys = append(keys, k)
	})

	fmt.Println("prometheus collect")

	for _, key := range keys {
		log := log.FromContext(ctx).With("target", key.String())
		log.Info("prometheus collect")
		tctx, err := r.targetStore.Get(ctx, key)
		if err != nil || tctx == nil {
			continue
		}
		cache := tctx.GetCache()
		if cache == nil {
			continue
		}
		log.Info("prometheus collect read all")
		notifications, err := cache.ReadAll()
		if err != nil {
			log.Error("cannot read from cache", "err", err)
			continue
		}
		for subName, notifs := range notifications {
			log.Info("prometheus collect", "subscription", subName)
			for _, notif := range notifs {
				log.Info("prometheus collect", "notif", notif)
				for _, update := range notif.GetUpdate() {
					log.Info("prometheus collect", "update", update)
					targetName := key.String()

					name, vname, labels, labelValues := r.getLabels("prefi", targetName, update.GetPath())

					val, err := getValue(update.GetVal())
					if err != nil {
						log.Error("prometheus collect cannot get typed value", "err", err)
						continue
					}

					floatVal, err := toFloat(val)
					if err != nil {
						log.Error("prometheus collect cannot translate to float", "err", err)
						continue
					}

					fmt.Println("prometheus collect", r.metricName(name, vname), labels, labelValues, floatVal)

					ch <- prometheus.MustNewConstMetric(
						prometheus.NewDesc(r.metricName(name, vname), "", labels, nil),
						prometheus.UntypedValue,
						floatVal,
						labelValues...)
				}
			}
		}
	}
}

func (r *PrometheusServer) metricName(name, valueName string) string {
	valueName = fmt.Sprintf("%s_%s", name, filepath.Base(valueName))
	return strings.TrimLeft(r.regex.ReplaceAllString(valueName, "_"), "_")
}

func (s *PrometheusServer) getLabels(prefix string, targetName string, p *gnmi.Path) (string, string, []string, []string) {
	// name of the last element of the path
	vname := s.regex.ReplaceAllString(filepath.Base(path.GnmiPathToXPath(p, false)), "_")
	name := ""

	// all the keys of the path
	labels := []string{"target_name"}
	// all the values of the keys in the path
	labelValues := []string{targetName}
	for i, pathElem := range p.GetElem() {
		// prometheus names should all be _ based
		//promName := strings.ReplaceAll(pathElem.GetName(), "-", "_")
		pathElemPromName := s.regex.ReplaceAllString(pathElem.GetName(), "_")
		if i == 0 {
			if prefix != "" {
				name = strings.Join([]string{prefix, pathElemPromName}, "_")
			} else {
				name = pathElemPromName
			}

		} else if i < len(p.GetElem())-1 {
			name = strings.Join([]string{name, pathElemPromName}, "_")
		}
		if len(pathElem.GetKey()) != 0 {
			for k, v := range pathElem.GetKey() {
				// append the keyName of the path
				labels = append(labels, strings.Join([]string{pathElemPromName, s.regex.ReplaceAllString(k, "_")}, "_"))
				// append the values of the keys
				labelValues = append(labelValues, v)
			}
		}
	}
	return name, vname, labels, labelValues

}

// getValue return the data of the gnmi typed value
func getValue(updValue *gnmi.TypedValue) (interface{}, error) {
	if updValue == nil {
		return nil, nil
	}
	var value interface{}
	var jsondata []byte
	switch updValue.Value.(type) {
	case *gnmi.TypedValue_AsciiVal:
		value = updValue.GetAsciiVal()
	case *gnmi.TypedValue_BoolVal:
		value = updValue.GetBoolVal()
	case *gnmi.TypedValue_BytesVal:
		value = updValue.GetBytesVal()
	case *gnmi.TypedValue_DecimalVal:
		value = updValue.GetDecimalVal()
	case *gnmi.TypedValue_FloatVal:
		value = updValue.GetFloatVal()
	case *gnmi.TypedValue_IntVal:
		value = updValue.GetIntVal()
	case *gnmi.TypedValue_StringVal:
		value = updValue.GetStringVal()
	case *gnmi.TypedValue_UintVal:
		value = updValue.GetUintVal()
	case *gnmi.TypedValue_JsonIetfVal:
		jsondata = updValue.GetJsonIetfVal()
	case *gnmi.TypedValue_JsonVal:
		jsondata = updValue.GetJsonVal()
	case *gnmi.TypedValue_LeaflistVal:
		value = updValue.GetLeaflistVal()
	case *gnmi.TypedValue_ProtoBytes:
		value = updValue.GetProtoBytes()
	case *gnmi.TypedValue_AnyVal:
		value = updValue.GetAnyVal()
	}
	if value == nil && len(jsondata) != 0 {
		err := json.Unmarshal(jsondata, &value)
		if err != nil {
			return nil, err
		}
	}
	return value, nil
}

func toFloat(v interface{}) (float64, error) {
	switch i := v.(type) {
	case float64:
		return float64(i), nil
	case float32:
		return float64(i), nil
	case int64:
		return float64(i), nil
	case int32:
		return float64(i), nil
	case int16:
		return float64(i), nil
	case int8:
		return float64(i), nil
	case uint64:
		return float64(i), nil
	case uint32:
		return float64(i), nil
	case uint16:
		return float64(i), nil
	case uint8:
		return float64(i), nil
	case int:
		return float64(i), nil
	case uint:
		return float64(i), nil
	case bool:
		if i {
			return 1, nil
		}
		return 0, nil
	case string:
		f, err := strconv.ParseFloat(i, 64)
		if err != nil {
			return math.NaN(), err
		}
		return f, err
		//lint:ignore SA1019 still need DecimalVal for backward compatibility
	default:
		return math.NaN(), errors.New("toFloat: unknown value is of incompatible type")
	}
}
