/*
Copyright 2023 The xxx Authors.

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

package v1alpha1

import (
	"bytes"
	"html/template"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetCondition returns the condition based on the condition kind
func (r *DiscoveryRule) GetCondition(t ConditionType) Condition {
	return r.Status.GetCondition(t)
}

// SetConditions sets the conditions on the resource. it allows for 0, 1 or more conditions
// to be set at once
func (r *DiscoveryRule) SetConditions(c ...Condition) {
	r.Status.SetConditions(c...)
}

// +k8s:deepcopy-gen=false
type DiscoveryRuleContext struct {
	Client                   client.Client
	DiscoveryRule            *DiscoveryRule // this is the original CR
	ConnectionProfile        *TargetConnectionProfile
	SyncProfile              *TargetSyncProfile
	SecretResourceVersion    string
	TLSSecretResourceVersion string
}

func (r *DiscoveryRule) GetTargetLabels(t *TargetSpec) (map[string]string, error) {
	if r.Spec.TargetTemplate == nil {
		return map[string]string{
			LabelKeyDiscoveryRule: r.GetName(),
		}, nil
	}
	return r.buildTags(r.Spec.TargetTemplate.Labels, t)
}

func (r *DiscoveryRule) GetTargetAnnotations(t *TargetSpec) (map[string]string, error) {
	if r.Spec.TargetTemplate == nil {
		return map[string]string{
			LabelKeyDiscoveryRule: r.GetName(),
		}, nil
	}
	return r.buildTags(r.Spec.TargetTemplate.Annotations, t)
}

func (r *DiscoveryRule) buildTags(m map[string]string, t *TargetSpec) (map[string]string, error) {
	// initialize map if empty
	if m == nil {
		m = make(map[string]string)
	}
	// add discovery-rule labels
	if t != nil {
		if _, ok := m[LabelKeyDiscoveryRule]; !ok {
			m[LabelKeyDiscoveryRule] = r.GetName()
		}
	}
	// render values templates
	labels := make(map[string]string, len(m))
	b := new(bytes.Buffer)
	for k, v := range m {
		tpl, err := template.New(k).Parse(v)
		if err != nil {
			return nil, err
		}
		b.Reset()
		err = tpl.Execute(b, t)
		if err != nil {
			return nil, err
		}
		labels[k] = b.String()
	}
	return labels, nil
}
