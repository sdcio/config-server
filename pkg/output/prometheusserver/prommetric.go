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
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/prometheus/prompb"
	targetmanager "github.com/sdcio/config-server/pkg/sdc/target/manager"
)

const (
	metricNameRegex   = "[^a-zA-Z0-9_]+"
	defaultMetricHelp = "sdcio generated metric"
)

var (
	MetricNameRegex = regexp.MustCompile(metricNameRegex)
)

type PromMetric struct {
	Name string
	Time *time.Time
	// AddedAt is used to expire metrics if the time field is not initialized
	// this happens when ExportTimestamp == false
	AddedAt time.Time

	labels []prompb.Label
	value  float64
}

// TODO get user input
func NewPromMetric(subName string, rt targetmanager.TargetRuntimeView, update *gnmi.Update) (*PromMetric, error) {
	val, err := getValue(update.GetVal())
	if err != nil {
		return nil, fmt.Errorf("prometheus metric cannot get typed value, err: %s", err)
	}
	floatVal, err := toFloat(val)
	if err != nil {
		return nil, fmt.Errorf("prometheus metric cannot get float value, err: %s", err)
	}
	metricName, labels := getPromPathData("", update.GetPath(), rt.PromLabels())

	return &PromMetric{
		Name:    metricName,
		labels:  labels,
		value:   floatVal,
		AddedAt: time.Now(),
	}, nil
}

// Metric
func (p *PromMetric) CalculateKey() uint64 {
	h := fnv.New64a()
	h.Write([]byte(p.Name))
	if len(p.labels) > 0 {
		h.Write([]byte(":"))
		sort.Slice(p.labels, func(i, j int) bool {
			return p.labels[i].Name < p.labels[j].Name
		})
		for _, label := range p.labels {
			h.Write([]byte(label.Name))
			h.Write([]byte(":"))
			h.Write([]byte(label.Value))
			h.Write([]byte(":"))
		}
	}
	return h.Sum64()
}

func (p *PromMetric) String() string {
	if p == nil {
		return ""
	}
	sb := strings.Builder{}
	sb.WriteString("name=")
	sb.WriteString(p.Name)
	sb.WriteString(",")
	numLabels := len(p.labels)
	if numLabels > 0 {
		sb.WriteString("labels=[")
		for i, lb := range p.labels {
			sb.WriteString(lb.Name)
			sb.WriteString("=")
			sb.WriteString(lb.Value)
			if i < numLabels-1 {
				sb.WriteString(",")
			}
		}
		sb.WriteString("],")
	}
	sb.WriteString(fmt.Sprintf("value=%f,", p.value))
	sb.WriteString("time=")
	if p.Time != nil {
		sb.WriteString(p.Time.String())
	} else {
		sb.WriteString("nil")
	}
	sb.WriteString(",addedAt=")
	sb.WriteString(p.AddedAt.String())
	return sb.String()
}

// Desc implements prometheus.Metric
func (p *PromMetric) Desc() *prometheus.Desc {
	labelNames := make([]string, 0, len(p.labels))
	for _, label := range p.labels {
		labelNames = append(labelNames, label.Name)
	}

	return prometheus.NewDesc(p.Name, defaultMetricHelp, labelNames, nil)
}

// Write implements prometheus.Metric
func (p *PromMetric) Write(out *dto.Metric) error {
	out.Untyped = &dto.Untyped{
		Value: &p.value,
	}
	out.Label = make([]*dto.LabelPair, 0, len(p.labels))
	for i := range p.labels {
		out.Label = append(out.Label, &dto.LabelPair{Name: &p.labels[i].Name, Value: &p.labels[i].Value})
	}
	if p.Time == nil {
		return nil
	}
	timestamp := p.Time.UnixNano() / 1000000
	out.TimestampMs = &timestamp
	return nil
}

func (r *PrometheusServer) MetricName(name, valueName string) string {
	valueName = fmt.Sprintf("%s_%s", name, filepath.Base(valueName))
	return strings.TrimLeft(r.regex.ReplaceAllString(valueName, "_"), "_")
}

// getPromPathData creates a metric name and augments the labels with the name of the
// keys and values
func getPromPathData(prefix string, p *gnmi.Path, labels []prompb.Label) (string, []prompb.Label) {
	sb := &strings.Builder{}
	elems := p.GetElem()
	numElems := len(elems)
	if prefix != "" {
		sb.WriteString(prefix)
		sb.WriteString("_")
	}
	for i, pathElem := range p.GetElem() {
		// replace non number and letters with _ -> prom convention
		pathElemPromName := MetricNameRegex.ReplaceAllString(pathElem.GetName(), "_")
		sb.WriteString(pathElemPromName)
		if i+1 != numElems {
			sb.WriteString("_")
		}

		if len(pathElem.GetKey()) != 0 {
			for k, v := range pathElem.GetKey() {
				keyPromName := MetricNameRegex.ReplaceAllString(k, "_")
				labels = append(labels, prompb.Label{
					Name:  strings.Join([]string{pathElemPromName, keyPromName}, "_"),
					Value: v,
				})
			}
		}
	}
	return sb.String(), labels
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
	default:
		return math.NaN(), fmt.Errorf("toFloat: unknown value is of incompatible type %s", reflect.TypeOf(i).Name())
	}
}
