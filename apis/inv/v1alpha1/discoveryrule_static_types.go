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

// DiscoveryRuleSpec defines the desired state of DiscoveryRule
type DiscoveryRuleStaticSpec struct {
	// Targets define the list of Targets(s)
	Targets []DiscoveryRuleStaticSpecTarget `json:"targets" yaml:"targets"`

	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="schema is immutable"
	// Schema the target uses
	Schema DiscoveryRuleStaticSpecSchema `json:"schema" yaml:"schema"`
}

type DiscoveryRuleStaticSpecTarget struct {
	// HostName defines the hostname of the target
	HostName string `json:"hostName" yaml:"hostName"`
	// IP defines the ip address of the target
	IP string `json:"ip" yaml:"address"`
}

type DiscoveryRuleStaticSpecSchema struct {
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="provider is immutable"
	// Provider of the Schema associated with the target
	Provider string `json:"provider" yaml:"provider"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="version is immutable"
	// Version of the Provider Schema associated with the target
	Version string `json:"version" yaml:"version"`
}

// +kubebuilder:object:root=true
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories={sdc,inv}
// DiscoveryRuleStatic is the Schema for the DiscoveryRuleStatic API
// +k8s:openapi-gen=true
type DiscoveryRuleStatic struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec DiscoveryRuleStaticSpec `json:"spec,omitempty" yaml:"spec,omitempty"`
}

// +kubebuilder:object:root=true
// DiscoveryRuleList contains a list of DiscoveryRuleStatic
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type DiscoveryRuleStaticList struct {
	metav1.TypeMeta `json:",inline" yaml:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Items           []DiscoveryRuleStatic `json:"items" yaml:"items"`
}

func init() {
	SchemeBuilder.Register(&DiscoveryRuleStatic{}, &DiscoveryRuleStaticList{})
}

var (
	DiscoveryRuleStaticKind             = reflect.TypeOf(DiscoveryRuleStatic{}).Name()
	DiscoveryRuleStaticGroupKind        = schema.GroupKind{Group: SchemeGroupVersion.Group, Kind: DiscoveryRuleStaticKind}.String()
	DiscoveryRuleStaticKindAPIVersion   = DiscoveryRuleStaticKind + "." + SchemeGroupVersion.String()
	DiscoveryRuleStaticGroupVersionKind = SchemeGroupVersion.WithKind(DiscoveryRuleStaticKind)
)
