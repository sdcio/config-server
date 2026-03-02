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
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DiscoveryProfileSpec defines the desired state of DiscoveryProfile
type DiscoveryVendorProfileSpec struct {
	Gnmi GnmiDiscoveryVendorProfileParameters `json:"gnmi" protobuf:"bytes,1,rep,name=gnmi"`
	// TODO netconf, others
}

type GnmiDiscoveryVendorProfileParameters struct {
	Organization string  `json:"organization" protobuf:"bytes,1,opt,name=organization"`
	ModelMatch   *string `json:"modelMatch,omitempty" protobuf:"bytes,2,opt,name=modelMatch"`
	//Paths        DiscoveryPaths `json:"paths" protobuf:"bytes,3,opt,name=paths"`
	// +listType=atomic
	Paths []DiscoveryPathDefinition `json:"paths" protobuf:"bytes,3,rep,name=paths"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="encoding is immutable"
	// +kubebuilder:validation:Enum=UNKNOWN;JSON;JSON_IETF;PROTO;ASCII;
	// +kubebuilder:default:=JSON_IETF
	Encoding *Encoding `json:"encoding,omitempty" yaml:"encoding,omitempty" protobuf:"bytes,4,opt,name=encoding,casttype=Encoding"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="preserveNamespace is immutable"
	// +kubebuilder:default:=true
	PreserveNamespace *bool `json:"preserveNamespace,omitempty" yaml:"preserveNamespace,omitempty" protobuf:"varint,5,opt,name=preserveNamespace"`
}

type DiscoveryPathDefinition struct {
	// Key defines the key of the path for fast lookup
	Key string `json:"key" protobuf:"bytes,1,opt,name=key"`
	// Path associated with the key
	Path string `json:"path" protobuf:"bytes,2,opt,name=path"`
	// Script defines the starlark script to transform the value
	Script *string `json:"script,omitempty" protobuf:"bytes,3,opt,name=script"`
	// Regex defines the regex to transform the value
	Regex *string `json:"regex,omitempty" protobuf:"bytes,4,opt,name=regex"`
}

// +kubebuilder:object:root=true
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:categories={sdc,inv}
// DiscoveryVendorProfile is the Schema for the DiscoveryVendorProfile API
// +k8s:openapi-gen=true
type DiscoveryVendorProfile struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec DiscoveryVendorProfileSpec `json:"spec,omitempty" yaml:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
}

// +kubebuilder:object:root=true
// DiscoveryVendorProfileList contains a list of DiscoveryVendorProfileList
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type DiscoveryVendorProfileList struct {
	metav1.TypeMeta `json:",inline" yaml:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" yaml:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []DiscoveryVendorProfile `json:"items" yaml:"items" protobuf:"bytes,2,rep,name=items"`
}

func init() {
	localSchemeBuilder.Register(&DiscoveryVendorProfile{}, &DiscoveryVendorProfileList{})
}

var (
	DiscoveryVendorProfileKind = reflect.TypeOf(DiscoveryVendorProfile{}).Name()
)
