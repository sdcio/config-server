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

package v1alpha1

import (
	"bytes"
	"errors"
	"fmt"
	"text/template"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func (r DiscoveryParameters) GetPeriod() metav1.Duration {
	if r.Period.Duration == 0 {
		return metav1.Duration{Duration: 1 * time.Minute}
	}
	return r.Period
}

func (r DiscoveryParameters) GetConcurrentScans() int64 {
	if r.ConcurrentScans == 0 {
		return 10
	}
	return r.ConcurrentScans
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
	// copy map
	cm := make(map[string]string, len(m))
	cm[LabelKeyDiscoveryRule] = name
	b := new(bytes.Buffer)
	for k, v := range m {
		tpl, err := template.New(k).Parse(v)
		if err != nil {
			return nil, err
		}
		err = tpl.Execute(b, nil)
		if err != nil {
			return nil, err
		}
		cm[k] = b.String()
		b.Reset()
	}
	return cm, nil
}
