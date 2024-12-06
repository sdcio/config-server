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
	"strconv"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sdcio/config-server/pkg/target"
)

// Describe implements prometheus.Collector
func (r *PrometheusServer) Describe(ch chan<- *prometheus.Desc) {}

func (r *PrometheusServer) Collect(ch chan<- prometheus.Metric) {
	keys := []storebackend.Key{}
	ctx := context.Background()

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

					val, err := getValue(update.GetVal())
					if err != nil {
						log.Error("cannot get typed value", "err", err)
						continue
					}

					floatVal, err := toFloat(val)
					if err != nil {
						log.Error("cannot translate to float", "err", err)
						continue
					}

					fmt.Println("prom metric", targetName, subName, update.GetPath(), update.GetVal(), floatVal)
				}
			}
		}
	}
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
