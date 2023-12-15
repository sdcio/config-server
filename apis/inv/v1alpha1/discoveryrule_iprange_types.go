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
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// +kubebuilder:validation:XValidation:rule="!has(oldSelf.cidrs) || has(self.cidrs)", message="cidr is required once set"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.excludes) || has(self.excludes)", message="excludes is required once set"
// DiscoveryRuleSpec defines the desired state of DiscoveryRule
type DiscoveryRuleIPRangeSpec struct {
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:XValidation:rule="oldSelf.all(x, x in self)",message="cidr is immutable"
	// list of CIDR(s) to be scanned
	CIDRs []string `json:"cidrs" yaml:"cidrs"`
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:XValidation:rule="oldSelf.all(x, x in self)",message="excludes is immutable"
	// IP CIDR(s) to be excluded
	Excludes []string `json:"excludes,omitempty" yaml:"excludes,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="concurrentScans is immutable"
	// +kubebuilder:default:=10
	// number of concurrent IP scan
	ConcurrentScans int64 `json:"concurrentScans,omitempty" yaml:"concurrentScans,omitempty"`
}

// +kubebuilder:object:root=true
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories={sdc,inv}
// DiscoveryRuleIPRange is the Schema for the DiscoveryRuleIPRange API
// +k8s:openapi-gen=true
type DiscoveryRuleIPRange struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec DiscoveryRuleIPRangeSpec `json:"spec,omitempty" yaml:"spec,omitempty"`
}

// +kubebuilder:object:root=true
// DiscoveryRuleList contains a list of DiscoveryRuleIPRange
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type DiscoveryRuleIPRangeList struct {
	metav1.TypeMeta `json:",inline" yaml:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Items           []DiscoveryRuleIPRange `json:"items" yaml:"items"`
}

func init() {
	SchemeBuilder.Register(&DiscoveryRuleIPRange{}, &DiscoveryRuleIPRangeList{})
}

var (
	DiscoveryRuleIPRangeKind             = reflect.TypeOf(DiscoveryRuleIPRange{}).Name()
	DiscoveryRuleIPRangeGroupKind        = schema.GroupKind{Group: SchemeGroupVersion.Group, Kind: DiscoveryRuleIPRangeKind}.String()
	DiscoveryRuleIPRangeKindAPIVersion   = DiscoveryRuleIPRangeKind + "." + SchemeGroupVersion.String()
	DiscoveryRuleIPRangeGroupVersionKind = SchemeGroupVersion.WithKind(DiscoveryRuleIPRangeKind)
)
