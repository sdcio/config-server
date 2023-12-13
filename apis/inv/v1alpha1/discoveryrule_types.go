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
type DiscoveryRuleSpec struct {
	// +kubebuilder:default:="1m"
	// Period defines the wait period between discovery rule runs
	Period metav1.Duration `json:"period" yaml:"period"`
	// +kubebuilder:default:=57400
	// Port defines the port on which the scan runs
	Port uint `json:"port" yaml:"port"`
	// Secret defines the name of the secret to connect to the target
	Secret string `json:"secret" yaml:"secret"`
	// TLSSecret defines the name of the TLS secret to connect to the target
	TLSSecret *string `json:"tlsSecret,omitempty" yaml:"tlsSecre,omitempty"`
	// ConnectionProfile defines the profile used to connect to the target
	ConnectionProfile string `json:"connectionProfile" yaml:"connectionProfile"`
	// SyncProfile defines the profile used to sync the config from the target
	SyncProfile string `json:"syncProfile" yaml:"syncProfile"`
	// DiscoveryRuleRef points to a specific implementation of the discovery rule
	// e.g. ip range or api or topology rule
	DiscoveryRuleRef ObjectReference `json:"discoveryRuleRef" yaml:"discoveryRuleRef"`
	// TargetTemplate defines the template we use to create the targets
	// as a result of the discovery
	TargetTemplate *TargetTemplate `json:"targetTemplate,omitempty" yaml:"targetTemplate,omitempty"`
}

type ObjectReference struct {
	// API version of the referent.
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	// Kind of the referent.
	Kind string `json:"kind" yaml:"kind"`
	// Name of the referent.
	Name string `json:"name" yaml:"name"`
}

// TargetTemplate defines the template of the target
type TargetTemplate struct {
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="nameTemplate is immutable"
	// target name template
	NameTemplate string `json:"nameTemplate,omitempty" yaml:"nameTemplate,omitempty"`

	// Annotations is a key value map to be copied to the target CR.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty" yaml:"annotations,omitempty"`

	// Labels is a key value map to be copied to the target CR.
	// +optional
	Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// DiscoveryRuleStatus defines the observed state of DiscoveryRule
type DiscoveryRuleStatus struct {
	// ConditionedStatus provides the status of the Discovery using conditions
	// 2 conditions are used:
	// - a condition for the reconcilation status
	// - a condition for the ready status
	// if both are true the other attributes in the status are meaningful
	ConditionedStatus `json:",inline" yaml:",inline"`
	// StartTime identifies when the dr got started
	StartTime metav1.Time `json:"startTime,omitempty" yaml:"startTime,omitempty"`
	// UsedReferences track the resource used to reconcile the cr
	UsedReferences *DiscoveryRuleStatusUsedReferences `json:"usedReferences,omitempty" yaml:"usedReferences,omitempty"`
}

type DiscoveryRuleStatusUsedReferences struct {
	SecretResourceVersion            string `json:"secretResourceVersion,omitempty" yaml:"secretResourceVersion,omitempty"`
	TLSSecretResourceVersion         string `json:"tlsSecretResourceVersion,omitempty" yaml:"tlsSecretResourceVersion,omitempty"`
	ConnectionProfileResourceVersion string `json:"connectionProfileResourceVersion" yaml:"connectionProfileResourceVersion"`
	SyncProfileResourceVersion       string `json:"syncProfileResourceVersion" yaml:"syncProfileResourceVersion"`
	DiscoveryRuleRefResourceVersion  string `json:"discoveryRuleRefResourceVersion" yaml:"discoveryRuleRefResourceVersion"`
}

// +kubebuilder:object:root=true
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:resource:categories={nephio,inv}
// DiscoveryRule is the Schema for the DiscoveryRule API
// +k8s:openapi-gen=true
type DiscoveryRule struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec   DiscoveryRuleSpec   `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status DiscoveryRuleStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

// +kubebuilder:object:root=true
// DiscoveryRuleList contains a list of DiscoveryRules
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type DiscoveryRuleList struct {
	metav1.TypeMeta `json:",inline" yaml:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Items           []DiscoveryRule `json:"items" yaml:"items"`
}

func init() {
	SchemeBuilder.Register(&DiscoveryRule{}, &DiscoveryRuleList{})
}

var (
	DiscoveryRuleKind             = reflect.TypeOf(DiscoveryRule{}).Name()
	DiscoveryRuleGroupKind        = schema.GroupKind{Group: SchemeGroupVersion.Group, Kind: DiscoveryRuleKind}.String()
	DiscoveryRuleKindAPIVersion   = DiscoveryRuleKind + "." + SchemeGroupVersion.String()
	DiscoveryRuleGroupVersionKind = SchemeGroupVersion.WithKind(DiscoveryRuleKind)
)
