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

type DiscoveryRuleSpecKind string

const (
	DiscoveryRuleSpecKindIP  DiscoveryRuleSpecKind = "ip"
	DiscoveryRuleSpecKindPOD DiscoveryRuleSpecKind = "pod"
	DiscoveryRuleSpecKindSVC DiscoveryRuleSpecKind = "svc"
)

// DiscoveryRuleSpec defines the desired state of DiscoveryRule
type DiscoveryRuleSpec struct {
	// +kubebuilder:validation:Enum=unknown;ip;pod;svc;
	// +kubebuilder:default:=ip
	Kind DiscoveryRuleSpecKind `json:"kind" yaml:"kind"`
	// IP Prefixes for which this discovery rule applies
	Prefixes []DiscoveryRulePrefix `json:"prefixes,omitempty" yaml:"prefixes,omitempty"`
	// Selector defines the selector used to select which POD/SVC are subject to this discovery rule
	Selector *metav1.LabelSelector `json:"selector,omitempty" yaml:"selector,omitempty"`
	// Discovery defines the generic parameters of the discovery rule
	DiscoveryParameters `json:",inline" yaml:",inline"`
}

type DiscoveryParameters struct {
	// Discovery rule defines the profiles and templates generic to any discovery rule class/type
	// +kubebuilder:default:=true
	// Discover defines if discovery is enabled or not
	Discover bool `json:"discover,omitempty" yaml:"discover,omitempty"`
	// DiscoveryProfile define the profiles the discovery controller uses to discover targets
	DiscoveryProfile *DiscoveryProfile `json:"discoveryProfile,omitempty" yaml:"discoveryProfile,omitempty"`
	// ConnectivityProfile define the profile the discovery controller uses to connect to targets
	// once discovered
	ConnectivityProfile ConnectivityProfile `json:"connectivityProfile" yaml:"connectivityProfile"`
	// TargetTemplate defines the template the discovery controller uses to create the targets as a result of the discovery
	TargetTemplate *TargetTemplate `json:"targetTemplate,omitempty" yaml:"targetTemplate,omitempty"`
	// +kubebuilder:default:="1m"
	// Period defines the wait period between discovery rule runs
	Period metav1.Duration `json:"period" yaml:"period"`
	// +kubebuilder:default:=10
	// number of concurrent IP scan
	ConcurrentScans int64 `json:"concurrentScans,omitempty" yaml:"concurrentScans,omitempty"`
}

type DiscoveryRulePrefix struct {
	// Prefix of the target/target(s)
	Prefix string `json:"prefix" yaml:"prefix"`
	// HostName of the ip prefix; used for /32 or /128 addresses with discovery disabled
	HostName string `json:"hostName,omitempty" yaml:"hostName,omitempty"`
	// IP Prefixes to be excluded
	Excludes []string `json:"excludes,omitempty" yaml:"excludes,omitempty"`
}

type DiscoveryProfile struct {
	Secret string `json:"secret" yaml:"secret"`
	// TLSSecret defines the name of the TLS secret to connect to the target
	TLSSecret *string `json:"tlsSecret,omitempty" yaml:"tlsSecret,omitempty"`
	// ConnectionProfiles define the list of profiles the discovery controller uses to discover the target.
	// The order in which they are specified is the order in which discovery is executed.
	ConnectionProfiles []string `json:"connectionProfiles" yaml:"connectionProfiles"`
}

type ConnectivityProfile struct {
	// Secret defines the name of the secret to connect to the target
	Secret string `json:"secret" yaml:"secret"`
	// TLSSecret defines the name of the TLS secret to connect to the target
	TLSSecret *string `json:"tlsSecret,omitempty" yaml:"tlsSecret,omitempty"`
	// ConnectionProfile define the profile used to connect to the target once discovered
	ConnectionProfile string `json:"connectionProfile" yaml:"connectionProfile"`
	// SyncProfile define the profile used to sync to the target config once discovered
	SyncProfile string `json:"syncProfile" yaml:"syncProfile"`
	// DefaultSchema define the default schema used to connect to a target
	// Used typically without discovery
	DefaultSchema *SchemaKey `json:"defaultSchema,omitempty" yaml:"defaultSchema,omitempty"`
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

type SchemaKey struct {
	// Provider specifies the provider of the schema.
	Provider string `json:"provider" yaml:"provider"`
	// Version defines the version of the schema
	Version string `json:"version" yaml:"version"`
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
}

/*
type DiscoveryRuleStatusUsedReferences struct {
	SecretResourceVersion             string                   `json:"secretResourceVersion,omitempty" yaml:"secretResourceVersion,omitempty"`
	TLSSecretResourceVersion          string                   `json:"tlsSecretResourceVersion,omitempty" yaml:"tlsSecretResourceVersion,omitempty"`
	Profiles                          []DiscoveryRuleProfile `json:"profiles,omitempty" yaml:"profiles,omitempty"`
	DiscoveryRuleRefResourceVersion string                   `json:"DiscoveryRuleRefResourceVersion" yaml:"DiscoveryRuleRefResourceVersion"`
}

type DiscoveryRuleProfile struct {
	ConnectionProfileResourceVersion string `json:"connectionProfileResourceVersion" yaml:"connectionProfileResourceVersion"`
	SyncProfileResourceVersion       string `json:"syncProfileResourceVersion" yaml:"syncProfileResourceVersion"`
}
*/

// +kubebuilder:object:root=true
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:resource:categories={sdc,inv}
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
