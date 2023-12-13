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

// TargetSpec defines the desired state of Target
type TargetSpec struct {
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="provider is immutable"
	// Provider specifies the provider using this target.
	Provider string `json:"provider" yaml:"provider"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="address is immutable"
	// Address defines the address to connect to the target
	Address string `json:"address" yaml:"address"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="secret is immutable"
	// Secret defines the name of the secret to connect to the target
	Secret string `json:"secret" yaml:"secret"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="tlsSecret is immutable"
	// TLSSecret defines the name of the TLS secret to connect to the target
	TLSSecret *string `json:"tlsSecret,omitempty" yaml:"tlsSecre,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="connectionProfile is immutable"
	// ConnectionProfile defines the profile used to connect to the target
	ConnectionProfile string `json:"connectionProfile" yaml:"connectionProfile"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="syncProfile is immutable"
	// SyncProfile defines the profile used to sync the config from the target
	SyncProfile string `json:"syncProfile" yaml:"syncProfile"`
}

// TargetStatus defines the observed state of Target
type TargetStatus struct {
	// ConditionedStatus provides the status of the Target using conditions
	// 2 conditions are used:
	// - a condition for the reconcilation status
	// - a condition for the ready status
	// if both are true the other attributes in the status are meaningful
	ConditionedStatus `json:",inline" yaml:",inline"`
	// Discovery info defines the information retrieved during discovery
	DiscoveryInfo *DiscoveryInfo `json:"discoveryInfo,omitempty" yaml:"discoveryInfo,omitempty"`
	// UsedReferences track the resource used to reconcile the cr
	UsedReferences *TargetStatusUsedReferences `json:"usedReferences,omitempty" yaml:"usedReferences,omitempty"`
}

type DiscoveryInfo struct {
	// Vendor associated with the target
	Vendor string `json:"vendor,omitempty"`
	// Type associated with the target
	Type string `json:"type,omitempty"`
	// HostName associated with the target
	HostName string `json:"hostname,omitempty"`
	// Platform associated with the target
	Platform string `json:"platform,omitempty"`
	// Version associated with the target
	Version string `json:"version,omitempty"`
	// MacAddress associated with the target
	MacAddress string `json:"macAddress,omitempty"`
	// SerialNumber associated with the target
	SerialNumber string `json:"serialNumber,omitempty"`
	// Supported Encodings of the target
	SupportedEncodings []string `json:"supportedEncodings,omitempty"`
	// Last discovery time
	LastSeen metav1.Time `json:"lastSeen,omitempty"`
}

type TargetStatusUsedReferences struct {
	SecretResourceVersion            string `json:"secretResourceVersion,omitempty" yaml:"secretResourceVersion,omitempty"`
	TLSSecretResourceVersion         string `json:"tlsSecretResourceVersion,omitempty" yaml:"tlsSecretResourceVersion,omitempty"`
	ConnectionProfileResourceVersion string `json:"connectionProfileResourceVersion" yaml:"connectionProfileResourceVersion"`
	SyncProfileResourceVersion       string `json:"syncProfileResourceVersion" yaml:"syncProfileResourceVersion"`
}

// +kubebuilder:object:root=true
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="DATASTORE",type="string",JSONPath=".status.conditions[?(@.type=='DSReady')].status"
// +kubebuilder:printcolumn:name="PLATFORM",type="string",JSONPath=".status.discoveryInfo.platform"
// +kubebuilder:printcolumn:name="VERSION",type="string",JSONPath=".status.discoveryInfo.version"
// +kubebuilder:printcolumn:name="VENDOR",type="string",JSONPath=".status.discoveryInfo.vendor"
// +kubebuilder:printcolumn:name="TYPE",type="string",JSONPath=".status.discoveryInfo.type"
// +kubebuilder:printcolumn:name="SERIALNUMBER",type="string",JSONPath=".status.discoveryInfo.serialNumber"
// +kubebuilder:printcolumn:name="MACADDRESS",type="string",JSONPath=".status.discoveryInfo.macAddress"
// +kubebuilder:resource:categories={nephio,inv}
// Target is the Schema for the Target API
// +k8s:openapi-gen=true
type Target struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec   TargetSpec   `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status TargetStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

// +kubebuilder:object:root=true
// TargetList contains a list of Targets
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type TargetList struct {
	metav1.TypeMeta `json:",inline" yaml:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Items           []Target `json:"items" yaml:"items"`
}

func init() {
	SchemeBuilder.Register(&Target{}, &TargetList{})
}

var (
	TargetKind             = reflect.TypeOf(Target{}).Name()
	TargetGroupKind        = schema.GroupKind{Group: SchemeGroupVersion.Group, Kind: TargetKind}.String()
	TargetKindAPIVersion   = TargetKind + "." + SchemeGroupVersion.String()
	TargetGroupVersionKind = SchemeGroupVersion.WithKind(TargetKind)
)
