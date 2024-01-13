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
	"errors"
	"fmt"
	"reflect"

	"github.com/henderiw/iputil"
	"github.com/iptecharch/config-server/pkg/testhelper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +k8s:deepcopy-gen=false
type DiscoveryObject interface {
	client.Object
	GetCondition(t ConditionType) Condition
	SetConditions(c ...Condition)
	Discovery() bool
	GetDiscoveryParameters() DiscoveryParameters
	GetDiscoveryKind() (DiscoveryRuleSpecKind, error)
	GetPrefixes() []DiscoveryRulePrefix
	GetAddresses() []DiscoveryRuleAddress
	GetPodSelector() metav1.LabelSelector
	GetSvcSelector() metav1.LabelSelector
	GetDefaultSchema() *SchemaKey
}

// +k8s:deepcopy-gen=false
// A DiscoveryObject is a list of discovery resources.
type DiscoveryObjectList interface {
	client.ObjectList
	// GetItems returns the list of discovery resources.
	GetItems() []DiscoveryObject
}

var _ DiscoveryObjectList = &DiscoveryRuleList{}

// Returns the generic discovery rule
func (r *DiscoveryRuleList) GetItems() []DiscoveryObject {
	l := make([]DiscoveryObject, len(r.Items))
	for i, item := range r.Items {
		item := item
		l[i] = &item
	}
	return l
}

var _ DiscoveryObject = &DiscoveryRule{}

// GetCondition returns the condition based on the condition kind
func (r *DiscoveryRule) GetCondition(t ConditionType) Condition {
	return r.Status.GetCondition(t)
}

// SetConditions sets the conditions on the resource. it allows for 0, 1 or more conditions
// to be set at once
func (r *DiscoveryRule) SetConditions(c ...Condition) {
	r.Status.SetConditions(c...)
}

func (r *DiscoveryRule) Discovery() bool {
	return r.Spec.DiscoveryParameters.DefaultSchema == nil
}

func (r *DiscoveryRule) GetDefaultSchema() *SchemaKey {
	return r.Spec.DiscoveryParameters.DefaultSchema
}

// Returns the generic discovery rule
func (r *DiscoveryRule) GetDiscoveryParameters() DiscoveryParameters {
	return r.Spec.DiscoveryParameters
}

func (r *DiscoveryRule) GetDiscoveryKind() (DiscoveryRuleSpecKind, error) {
	kinds := []DiscoveryRuleSpecKind{}
	if len(r.Spec.Addresses) != 0 {
		kinds = append(kinds, DiscoveryRuleSpecKindAddress)
	}
	if len(r.Spec.Prefixes) != 0 {
		kinds = append(kinds, DiscoveryRuleSpecKindPrefix)
	}
	if r.Spec.PodSelector != nil {
		kinds = append(kinds, DiscoveryRuleSpecKindPod)
	}
	if r.Spec.ServiceSelector != nil {
		kinds = append(kinds, DiscoveryRuleSpecKindSvc)
	}
	if len(kinds) == 0 {
		return DiscoveryRuleSpecKindUnknown, fmt.Errorf("a discovery rule need specify either addresses, prefixes, podSelector or serviceSelector, got neither of them")
	}
	if len(kinds) != 1 {
		return DiscoveryRuleSpecKindUnknown, fmt.Errorf("a discovery rule can only have 1 discovery kind, got: %v", kinds)
	}
	return kinds[0], nil
}

func (r *DiscoveryRule) GetPrefixes() []DiscoveryRulePrefix {
	return r.Spec.Prefixes
}

func (r *DiscoveryRule) GetAddresses() []DiscoveryRuleAddress {
	return r.Spec.Addresses
}

func (r *DiscoveryRule) GetPodSelector() metav1.LabelSelector {
	if r.Spec.PodSelector == nil {
		return metav1.LabelSelector{}
	}
	return *r.Spec.PodSelector
}

func (r *DiscoveryRule) GetSvcSelector() metav1.LabelSelector {
	if r.Spec.PodSelector == nil {
		return metav1.LabelSelector{}
	}
	return *r.Spec.ServiceSelector
}

func (r *DiscoveryRule) Validate() error {

	kind, err := r.GetDiscoveryKind()
	if err != nil {
		return err
	}
	var errm error
	if r.Spec.DiscoveryParameters.DefaultSchema != nil {
		// discovery disabled -> prefix based discovery cannot be supported
		if kind == DiscoveryRuleSpecKindPrefix {
			errm = errors.Join(errm, fmt.Errorf("prefix based discovery cannot have discovery disabled, remove the default schema for prefix based discovery"))
		}
	}
	// prefixes need a valid ip
	for _, p := range r.Spec.Prefixes {
		_, err := iputil.New(p.Prefix)
		if err != nil {
			errm = errors.Join(errm, fmt.Errorf("parsing prefix %s failed %s", p.Prefix, err.Error()))
			continue
		}
	}

	if err := r.Spec.DiscoveryParameters.Validate(); err != nil {
		errm = errors.Join(errm, err)
	}
	return errm
}

// GetConfigSetFromFile is a helper for tests to use the
// examples and validate them in unit tests
func GetDiscoveryRuleFromFile(path string) (*DiscoveryRule, error) {
	addToScheme := AddToScheme
	obj := &DiscoveryRule{}
	gvk := SchemeGroupVersion.WithKind(reflect.TypeOf(obj).Name())
	// build object from file
	if err := testhelper.GetKRMResource(path, obj, gvk, addToScheme); err != nil {
		return nil, err
	}
	return obj, nil
}
