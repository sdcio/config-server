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

	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type DiscoveryRuleSpecKind string

const (
	DiscoveryRuleSpecKindPrefix  DiscoveryRuleSpecKind = "prefix"
	DiscoveryRuleSpecKindAddress DiscoveryRuleSpecKind = "address"
	DiscoveryRuleSpecKindPod     DiscoveryRuleSpecKind = "pod"
	DiscoveryRuleSpecKindSvc     DiscoveryRuleSpecKind = "svc"
	DiscoveryRuleSpecKindUnknown DiscoveryRuleSpecKind = "unknown"
)

// DiscoveryRuleSpec defines the desired state of DiscoveryRule
type DiscoveryRuleSpec struct {
	// IP Prefixes for which this discovery rule applies
	// +listType=atomic
	Prefixes []DiscoveryRulePrefix `json:"prefixes,omitempty" protobuf:"bytes,1,rep,name=prefixes"`
	// IP Prefixes for which this discovery rule applies
	// +listType=atomic
	Addresses []DiscoveryRuleAddress `json:"addresses,omitempty" protobuf:"bytes,2,rep,name=addresses"`
	// PodSelector defines the pod selector for which this discovery rule applies
	PodSelector *metav1.LabelSelector `json:"podSelector,omitempty" protobuf:"bytes,3,opt,name=podSelector"`
	// ServiceSelector defines the service selector for which this discovery rule applies
	ServiceSelector *metav1.LabelSelector `json:"serviceSelector,omitempty" protobuf:"bytes,4,opt,name=serviceSelector"`
	// ServiceDomain defines the service domain of the cluster, used by svc discovery to identify the
	// domain name in the k8s cluster where the service reside.
	ServiceDomain string `json:"serviceDomain,omitempty" protobuf:"bytes,5,opt,name=serviceDomain"`
	// Discovery defines the generic parameters of the discovery rule
	DiscoveryParameters `json:",inline" protobuf:"bytes,6,opt,name=discoveryParameters"`
}

type DiscoveryParameters struct {
	// DefaultSchema define the default schema used to connect to a target
	// Indicates that discovery is disable; cannot be used for prefix based discovery rules
	DefaultSchema *SchemaKey `json:"defaultSchema,omitempty" protobuf:"bytes,1,opt,name=defaultSchema"`
	// DiscoveryProfile define the profiles the discovery controller uses to discover targets
	DiscoveryProfile *DiscoveryProfile `json:"discoveryProfile,omitempty" protobuf:"bytes,2,opt,name=discoveryProfile"`
	// TargetConnectionProfiles define the profile the discovery controller uses to create targets
	// once discovered
	// +listType=atomic
	TargetConnectionProfiles []TargetProfile `json:"targetConnectionProfiles" protobuf:"bytes,3,rep,name=targetConnectionProfiles"`
	// TargetTemplate defines the template the discovery controller uses to create the targets as a result of the discovery
	TargetTemplate *TargetTemplate `json:"targetTemplate,omitempty" protobuf:"bytes,4,opt,name=targetTemplate"`
	// Period defines the wait period between discovery rule runs
	Period metav1.Duration `json:"period,omitempty" protobuf:"bytes,5,opt,name=period"`
	// number of concurrent IP scan
	ConcurrentScans int64 `json:"concurrentScans,omitempty" protobuf:"varint,6,opt,name=concurrentScans"`
}

type DiscoveryRulePrefix struct {
	// Prefix of the target/target(s)
	Prefix string `json:"prefix" protobuf:"bytes,1,opt,name=prefix"`
	// IP Prefixes to be excluded
	// +listType=atomic
	Excludes []string `json:"excludes,omitempty" protobuf:"bytes,2,rep,name=excludes"`
}

type DiscoveryRuleAddress struct {
	// Address (specified as IP or DNS name) of the target/target(s)
	Address string `json:"address" protobuf:"bytes,1,opt,name=address"`
	// HostName of the ip prefix; used for /32 or /128 addresses with discovery disabled
	HostName string `json:"hostName,omitempty" protobuf:"bytes,2,opt,name=hostName"`
}

type DiscoveryProfile struct {
	// Credentials defines the name of the secret that holds the credentials to connect to the target
	Credentials string `json:"credentials" protobuf:"bytes,1,opt,name=credentials"`
	// TLSSecret defines the name of the TLS secret to connect to the target if mtls is used
	TLSSecret *string `json:"tlsSecret,omitempty" protobuf:"bytes,2,opt,name=tlsSecret"`
	// ConnectionProfiles define the list of profiles the discovery controller uses to discover the target.
	// The order in which they are specified is the order in which discovery is executed.
	// +listType=atomic
	ConnectionProfiles []string `json:"connectionProfiles" protobuf:"bytes,3,rep,name=connectionProfiles"`
}

type TargetProfile struct {
	// Credentials defines the name of the secret that holds the credentials to connect to the target
	Credentials string `json:"credentials" protobuf:"bytes,1,opt,name=credentials"`
	// TLSSecret defines the name of the TLS secret to connect to the target if mtls is used
	TLSSecret *string `json:"tlsSecret,omitempty" protobuf:"bytes,2,opt,name=tlsSecret"`
	// ConnectionProfile define the profile used to connect to the target once discovered
	ConnectionProfile string `json:"connectionProfile" protobuf:"bytes,3,opt,name=connectionProfile"`
	// SyncProfile define the profile used to sync to the target config once discovered
	SyncProfile *string `json:"syncProfile,omitempty" protobuf:"bytes,4,opt,name=syncProfile"`
}

// TargetTemplate defines the template of the target
type TargetTemplate struct {
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="nameTemplate is immutable"
	// target name template
	NameTemplate string `json:"nameTemplate,omitempty" protobuf:"bytes,1,opt,name=nameTemplate"`
	// Annotations is a key value map to be copied to the target CR.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty" protobuf:"bytes,2,rep,name=annotations"`
	// Labels is a key value map to be copied to the target CR.
	// +optional
	Labels map[string]string `json:"labels,omitempty" protobuf:"bytes,3,rep,name=labels"`
}

type SchemaKey struct {
	// Provider specifies the provider of the schema.
	Provider string `json:"provider" protobuf:"bytes,1,opt,name=provider"`
	// Version defines the version of the schema
	Version string `json:"version" protobuf:"bytes,2,opt,name=version"`
}

// DiscoveryRuleStatus defines the observed state of DiscoveryRule
type DiscoveryRuleStatus struct {
	// ConditionedStatus provides the status of the Discovery using conditions
	// 2 conditions are used:
	// - a condition for the reconcilation status
	// - a condition for the ready status
	// if both are true the other attributes in the status are meaningful
	condv1alpha1.ConditionedStatus `json:",inline" protobuf:"bytes,1,opt,name=conditionedStatus"`
	// StartTime identifies when the dr got started
	StartTime *metav1.Time `json:"startTime,omitempty" protobuf:"bytes,2,opt,name=startTime"`
}

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
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec   DiscoveryRuleSpec   `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	Status DiscoveryRuleStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// +kubebuilder:object:root=true
// DiscoveryRuleList contains a list of DiscoveryRules
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type DiscoveryRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []DiscoveryRule `json:"items" protobuf:"bytes,2,rep,name=items"`
}

func init() {
	localSchemeBuilder.Register(&DiscoveryRule{}, &DiscoveryRuleList{})
}

var (
	DiscoveryRuleKind           = reflect.TypeOf(DiscoveryRule{}).Name()
	DiscoveryRuleGroupKind      = schema.GroupKind{Group: SchemeGroupVersion.Group, Kind: DiscoveryRuleKind}.String()
	DiscoveryRuleKindAPIVersion = DiscoveryRuleKind + "." + SchemeGroupVersion.String()
	//DiscoveryRuleGroupVersionKind = SchemeGroupVersion.WithKind(DiscoveryRuleKind)
)
