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

// TargetSpec defines the desired state of Target
type TargetSpec struct {
	// Provider specifies the provider using this target.
	Provider string `json:"provider" protobuf:"bytes,1,opt,name=provider"`
	// Address defines the address to connect to the target
	Address string `json:"address" protobuf:"bytes,2,opt,name=address"`
	// TargetProfile defines the Credentials/TLSSecret and sync/connectivity profile to connect to the target
	TargetProfile `json:",inline" protobuf:"bytes,3,opt,name=targetProfile"`
}

// TargetStatus defines the observed state of Target
type TargetStatus struct {
	// ConditionedStatus provides the status of the Target using conditions
	condv1alpha1.ConditionedStatus `json:",inline" protobuf:"bytes,1,opt,name=conditionedStatus"`
	// Discovery info defines the information retrieved during discovery
	DiscoveryInfo *DiscoveryInfo `json:"discoveryInfo,omitempty" protobuf:"bytes,2,opt,name=discoveryInfo"`
	// UsedReferences track the resource used to reconcile the cr
	UsedReferences *TargetStatusUsedReferences `json:"usedReferences,omitempty" protobuf:"bytes,3,opt,name=usedReferences"`
	// ResourceVersion used by recovery
	ResourceVersion *string `json:"resourceVersion" protobuf:"bytes,4,opt,name=resourceVersion"`
	// Generation used by recovery
	Generation *int64 `json:"generation" protobuf:"bytes,5,opt,name=generation"`
}

type DiscoveryInfo struct {
	// Protocol used for discovery
	Protocol string `json:"protocol,omitempty" protobuf:"bytes,1,opt,name=protocol"`
	// Type associated with the target
	Provider string `json:"provider,omitempty" protobuf:"bytes,2,opt,name=provider"`
	// Version associated with the target
	Version string `json:"version,omitempty" protobuf:"bytes,3,opt,name=version"`
	// HostName associated with the target
	HostName string `json:"hostname,omitempty" protobuf:"bytes,4,opt,name=hostname"`
	// Platform associated with the target
	Platform string `json:"platform,omitempty" protobuf:"bytes,5,opt,name=platform"`
	// MacAddress associated with the target
	MacAddress string `json:"macAddress,omitempty" protobuf:"bytes,6,opt,name=macAddress"`
	// SerialNumber associated with the target
	SerialNumber string `json:"serialNumber,omitempty" protobuf:"bytes,7,opt,name=serialNumber"`
	// Supported Encodings of the target
	SupportedEncodings []string `json:"supportedEncodings,omitempty" protobuf:"bytes,8,rep,name=supportedEncodings"`
	// Last discovery time
	//LastSeen metav1.Time `json:"lastSeen,omitempty"`
}

type TargetStatusUsedReferences struct {
	SecretResourceVersion            string `json:"secretResourceVersion,omitempty" yaml:"secretResourceVersion,omitempty" protobuf:"bytes,1,opt,name=secretResourceVersion"`
	TLSSecretResourceVersion         string `json:"tlsSecretResourceVersion,omitempty" yaml:"tlsSecretResourceVersion,omitempty" protobuf:"bytes,2,opt,name=tlsSecretResourceVersion"`
	ConnectionProfileResourceVersion string `json:"connectionProfileResourceVersion" yaml:"connectionProfileResourceVersion" protobuf:"bytes,3,opt,name=connectionProfileResourceVersion"`
	SyncProfileResourceVersion       string `json:"syncProfileResourceVersion" yaml:"syncProfileResourceVersion" protobuf:"bytes,4,opt,name=syncProfileResourceVersion"`
}

// +kubebuilder:object:root=true
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="REASON",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].message"
// +kubebuilder:printcolumn:name="PROVIDER",type="string",JSONPath=".spec.provider"
// +kubebuilder:printcolumn:name="VERSION",type="string",JSONPath=".status.discoveryInfo.version"
// +kubebuilder:printcolumn:name="ADDRESS",type="string",JSONPath=".spec.address"
// +kubebuilder:printcolumn:name="PLATFORM",type="string",JSONPath=".status.discoveryInfo.platform"
// +kubebuilder:printcolumn:name="SERIALNUMBER",type="string",JSONPath=".status.discoveryInfo.serialNumber"
// +kubebuilder:printcolumn:name="MACADDRESS",type="string",JSONPath=".status.discoveryInfo.macAddress"
// +kubebuilder:resource:categories={sdc,inv}
// Target is the Schema for the Target API
// +k8s:openapi-gen=true
type Target struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec   TargetSpec   `json:"spec,omitempty" yaml:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	Status TargetStatus `json:"status,omitempty" yaml:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// +kubebuilder:object:root=true
// TargetList contains a list of Targets
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type TargetList struct {
	metav1.TypeMeta `json:",inline" yaml:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" yaml:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []Target `json:"items" yaml:"items" protobuf:"bytes,2,rep,name=items"`
}

func init() {
	localSchemeBuilder.Register(&Target{}, &TargetList{})
}

var (
	TargetKind             = reflect.TypeOf(Target{}).Name()
	TargetGroupKind        = schema.GroupKind{Group: SchemeGroupVersion.Group, Kind: TargetKind}.String()
	TargetKindAPIVersion   = TargetKind + "." + SchemeGroupVersion.String()
	TargetGroupVersionKind = SchemeGroupVersion.WithKind(TargetKind)
)
