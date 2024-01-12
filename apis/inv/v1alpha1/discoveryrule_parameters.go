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
	"errors"
	"fmt"
	"text/template"
)

func (r DiscoveryParameters) Validate() error {
	var errm error
	if r.DefaultSchema != nil {
		if r.DefaultSchema.Provider == "" ||
			r.DefaultSchema.Version == "" {
			errm = errors.Join(errm, fmt.Errorf("a default schema needs a provider and version, got provider: %s, version: %s",
				r.DefaultSchema.Provider,
				r.DefaultSchema.Version,
			))
		}
	} else {
		if r.DiscoveryProfile == nil {
			errm = errors.Join(errm, fmt.Errorf("cannot discover without a discovery profile"))
		} else {
			if r.DiscoveryProfile.Credentials == "" {
				errm = errors.Join(errm, fmt.Errorf("cannot discover without a secret in the discovery profile"))
			}
			if len(r.DiscoveryProfile.ConnectionProfiles) == 0 {
				errm = errors.Join(errm, fmt.Errorf("cannot discover without a connectionProfile in the discovery profile"))
			}
		}
	}
	return errm
}

func (r DiscoveryParameters) GetTargetLabels(name string) (map[string]string, error) {
	if r.TargetTemplate == nil {
		return map[string]string{
			LabelKeyDiscoveryRule: name,
		}, nil
	}
	return r.buildTags(r.TargetTemplate.Labels, name)
}

func (r DiscoveryParameters) GetTargetAnnotations(name string) (map[string]string, error) {
	if r.TargetTemplate == nil {
		return map[string]string{
			LabelKeyDiscoveryRule: name,
		}, nil
	}
	return r.buildTags(r.TargetTemplate.Annotations, name)
}

func (r DiscoveryParameters) buildTags(m map[string]string, name string) (map[string]string, error) {
	// initialize map if empty
	if m == nil {
		m = make(map[string]string)
	}
	// add discovery-rule labels
	if _, ok := m[LabelKeyDiscoveryRule]; !ok {
		m[LabelKeyDiscoveryRule] = name
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
		err = tpl.Execute(b, nil)
		if err != nil {
			return nil, err
		}
		labels[k] = b.String()
	}
	return labels, nil
}
