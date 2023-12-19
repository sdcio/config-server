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

	"github.com/henderiw/iputil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +k8s:deepcopy-gen=false
type DiscoveryObject interface {
	client.Object
	GetCondition(t ConditionType) Condition
	SetConditions(c ...Condition)
	GetDiscoveryParameters() DiscoveryParameters
	GetDiscoveryKind() DiscoveryRuleSpecKind
	GetPrefixes() []DiscoveryRulePrefix
	GetLabelSelectors() metav1.LabelSelector
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

// Returns the generic discovery rule
func (r *DiscoveryRule) GetDiscoveryParameters() DiscoveryParameters {
	return r.Spec.DiscoveryParameters
}

func (r *DiscoveryRule) GetDiscoveryKind() DiscoveryRuleSpecKind {
	return r.Spec.Kind
}

func (r *DiscoveryRule) GetPrefixes() []DiscoveryRulePrefix {
	return r.Spec.Prefixes
}

func (r *DiscoveryRule) GetLabelSelectors() metav1.LabelSelector {
	if r.Spec.Selector == nil {
		return metav1.LabelSelector{}
	}
	return *r.Spec.Selector
}	

func (r *DiscoveryRule) Validate() error {
	var errm error
	// when discovery is diabled
	// 1. the prefix need to be a /32 or /128
	// 2. a hostname is needed
	if !r.Spec.DiscoveryParameters.Discover {
		for _, p := range r.Spec.Prefixes {
			if p.HostName == "" {
				errm = errors.Join(errm, fmt.Errorf("when discovery is disabled, the prefix %s need to have a hostname", p.Prefix))
			}
			ipp, err := iputil.New(p.Prefix)
			if err != nil {
				errm = errors.Join(errm, fmt.Errorf("parsing prefix %s failed %s", p.Prefix, err.Error()))
				continue
			}
			if !ipp.IsAddressPrefix() {
				errm = errors.Join(errm, fmt.Errorf("when discovery is disabled, the prefix need to be an address, got: %s", p.Prefix))
			}
		}
	} else {
		for _, p := range r.Spec.Prefixes {
			_, err := iputil.New(p.Prefix)
			if err != nil {
				errm = errors.Join(errm, fmt.Errorf("parsing prefix %s failed %s", p.Prefix, err.Error()))
				continue
			}
		}
	}

	switch r.Spec.Kind {
	case DiscoveryRuleSpecKindIP:
		if len(r.Spec.Prefixes) == 0 {
			errm = errors.Join(errm, fmt.Errorf("an %s discovery kind cannot discover without ip prefixes", string(r.Spec.Kind)))
		}
	case DiscoveryRuleSpecKindPOD, DiscoveryRuleSpecKindSVC:
		if r.Spec.Selector == nil {
			errm = errors.Join(errm, fmt.Errorf("an %s discovery kind cannot discover without a selector", string(r.Spec.Kind)))
		} else {
			if len(r.Spec.Selector.MatchExpressions) == 0 && len(r.Spec.Selector.MatchLabels) == 0 {
				errm = errors.Join(errm, fmt.Errorf("an %s discovery kind cannot discover without selector criteria", string(r.Spec.Kind)))
			}
		}
	default:
		errm = errors.Join(errm, fmt.Errorf("unsupported discovery kind, got: %s", string(r.Spec.Kind)))
	}

	if err := r.Spec.DiscoveryParameters.Validate(); err != nil {
		errm = errors.Join(errm, err)
	}
	return errm
}
