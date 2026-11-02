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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// ConfigSpec defines the desired state of Config
type ConfigSpec struct {
	// Lifecycle determines the lifecycle policies the resource e.g. delete is orphan or delete
	// will follow
	Lifecycle *Lifecycle `json:"lifecycle,omitempty" protobuf:"bytes,1,opt,name=lifecycle"`
	// Priority defines the priority of this config
	Priority int32 `json:"priority,omitempty" protobuf:"varint,2,opt,name=priority"`
	// Revertive defines if this CR is enabled for revertive or non revertve operation
	Revertive *bool `json:"revertive,omitempty" protobuf:"varint,3,opt,name=revertive"`
	// Config defines the configuration to be applied to a target device
	// +kubebuilder:pruning:PreserveUnknownFields
	// +listType=atomic
	Config []ConfigBlob `json:"config" protobuf:"bytes,4,rep,name=config"`
	// Vars declares named variables referenced from Config blob values via the
	// ${vars.<name>} placeholder. The controller resolves each variable and
	// substitutes its value into the rendered configuration before it is sent to
	// the target. Variables are the supported mechanism for injecting sensitive
	// material: the referenced Secret value is resolved, encrypted into the
	// SensitiveConfig payload, and every leaf it lands on is recorded as a
	// sensitive path so the datastore redacts it in northbound responses.
	// A Config with no variables is rendered verbatim.
	// +optional
	// +listType=map
	// +listMapKey=name
	Vars []ConfigVar `json:"vars" protobuf:"bytes,5,rep,name=vars"`
}

type ConfigBlob struct {
	// Path defines the path relative to which the value is applicable
	Path string `json:"path,omitempty" protobuf:"bytes,1,opt,name=config"`
	// +kubebuilder:pruning:PreserveUnknownFields
	// +structType=atomic
	Value runtime.RawExtension `json:"value" protobuf:"bytes,2,opt,name=value"`
}

// ConfigVar is a named variable referenced from Config blob values as
// ${vars.<name>}. The name is the merge key for the Vars list and must be
// unique within a single Config. Exactly one value source must be set; today
// the only source is SecretRef.
type ConfigVar struct {
	// Name identifies the variable for reference as ${vars.<name>}.
	// Must be unique within the Config's Vars list.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9_.-]+$`
	Name string `json:"name" protobuf:"bytes,1,opt,name=name"`

	// SecretRef resolves the variable's value from a key of a Secret in the
	// same namespace as the Config. The resolved value is never stored in
	// clear: it is encrypted into the SensitiveConfig payload, and the leaves
	// it is substituted into are tracked as sensitive paths.
	// +optional
	SecretRef *corev1.SecretKeySelector `json:"secretRef,omitempty" protobuf:"bytes,2,opt,name=secretRef"`
}

// ConfigStatus defines the observed state of Config
type ConfigStatus struct {
	// ConditionedStatus provides the status of the Readiness using conditions
	// if the condition is true the other attributes in the status are meaningful
	condv1alpha1.ConditionedStatus `json:",inline" protobuf:"bytes,1,opt,name=conditionedStatus"`
}

type ConfigStatusLastKnownGoodSchema struct {
	// Schema Type
	Type string `json:"type,omitempty" protobuf:"bytes,1,opt,name=type"`
	// Schema Vendor
	Vendor string `json:"vendor,omitempty" protobuf:"bytes,2,opt,name=vendor"`
	// Schema Version
	Version string `json:"version,omitempty" protobuf:"bytes,3,opt,name=version"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories={sdc}

// Config defines the Schema for the Config API
type Config struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec   ConfigSpec   `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	Status ConfigStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// ConfigList contains a list of Configs
type ConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []Config `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// Config type metadata.
var (
	ConfigKind = reflect.TypeOf(Config{}).Name()
)
