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
	corev1 "k8s.io/api/core/v1"
)

// SensitiveConfigSpec defines the desired state of SensitiveConfig
type SensitiveConfigSpec struct {
	// Lifecycle determines the lifecycle policies the resource e.g. delete is orphan or delete
	// will follow
	Lifecycle *Lifecycle `json:"lifecycle,omitempty" protobuf:"bytes,1,opt,name=lifecycle"`
	// Priority defines the priority of this SensitiveConfig
	Priority int64 `json:"priority,omitempty" protobuf:"varint,2,opt,name=priority"`
	// Revertive defines if this CR is enabled for revertive or non revertve operation
	Revertive *bool `json:"revertive,omitempty" protobuf:"varint,3,opt,name=revertive"`
	// SensitiveConfig defines the SensitiveConfiguration to be applied to a target device
	Config []SensitiveConfigData `json:"config" protobuf:"bytes,4,rep,name=config"`
}

type SensitiveConfigData struct {
	// Path defines the path relative to which the value is applicable
	Path string `json:"path" protobuf:"bytes,1,opt,name=SensitiveConfig"`
	// SecretKeyRef refers to a secret in the same namesapce as the config
	SecretKeyRef corev1.SecretKeySelector `json:"secretKeyRef" protobuf:"bytes,2,opt,name=secretKeyRef"`
}

// SensitiveConfigStatus defines the observed state of SensitiveConfig
type SensitiveConfigStatus struct {
	// ConditionedStatus provides the status of the Readiness using conditions
	// if the condition is true the other attributes in the status are meaningful
	condv1alpha1.ConditionedStatus `json:",inline" protobuf:"bytes,1,opt,name=conditionedStatus"`
	// LastKnownGoodSchema identifies the last known good schema used to apply the SensitiveConfig successfully
	LastKnownGoodSchema *ConfigStatusLastKnownGoodSchema `json:"lastKnownGoodSchema,omitempty" protobuf:"bytes,2,opt,name=lastKnownGoodSchema"`
	// AppliedSensitiveConfig defines the SensitiveConfig applied to the target
	AppliedSensitiveConfig *SensitiveConfigSpec `json:"appliedSensitiveConfig,omitempty" protobuf:"bytes,3,opt,name=appliedSensitiveConfig"`
	// Deviations generation used for the latest SensitiveConfig apply
	DeviationGeneration *int64 `json:"deviationGeneration,omitempty" protobuf:"bytes,4,opt,name=deviationGeneration"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories={sdc}

// SensitiveConfig defines the Schema for the SensitiveConfig API
type SensitiveConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec   SensitiveConfigSpec   `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	Status SensitiveConfigStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// SensitiveConfigList contains a list of SensitiveConfigs
type SensitiveConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []SensitiveConfig `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// SensitiveConfig type metadata.
var (
	SensitiveConfigKind = reflect.TypeOf(SensitiveConfig{}).Name()
)
