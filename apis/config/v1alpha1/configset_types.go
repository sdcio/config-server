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
)

// ConfigSetSpec defines the desired state of Config
type ConfigSetSpec struct {
	// Targets defines the targets on which this configSet applies
	Target Target `json:"target" protobuf:"bytes,1,opt,name=target"`
	// Lifecycle determines the lifecycle policies the resource e.g. delete is orphan or delete
	// will follow
	Lifecycle *Lifecycle `json:"lifecycle,omitempty" protobuf:"bytes,2,opt,name=lifecycle"`
	// Priority defines the priority of this config
	Priority int64 `json:"priority,omitempty" protobuf:"varint,3,opt,name=priority"`
	// Revertive defines if this CR is enabled for revertive or non revertve operation
	Revertive *bool `json:"revertive,omitempty" protobuf:"varint,4,opt,name=revertive"`
	// Config defines the configuration to be applied to a target device
	//+kubebuilder:pruning:PreserveUnknownFields
	// +listType=atomic
	Config []ConfigBlob `json:"config" protobuf:"bytes,5,rep,name=config"`
}

type Target struct {
	// TargetSelector defines the selector used to select the targets to which the config applies
	TargetSelector *metav1.LabelSelector `json:"targetSelector,omitempty" protobuf:"bytes,1,opt,name=targetSelector"`
}

// ConfigSetStatus defines the observed state of Config
type ConfigSetStatus struct {
	// ConditionedStatus provides the status of the Readiness using conditions
	// if the condition is true the other attributes in the status are meaningful
	condv1alpha1.ConditionedStatus `json:",inline" protobuf:"bytes,1,opt,name=conditionedStatus"`
	// Targets defines the status of the configSet resource on the respective target
	// +listType=atomic
	Targets []TargetStatus `json:"targets,omitempty" protobuf:"bytes,2,rep,name=targets"`
}

type TargetStatus struct {
	Name string `json:"name" protobuf:"bytes,1,opt,name=name"`
	// right now we assume the namespace of the config and target are aligned
	//NameSpace string `json:"namespace" protobuf:"bytes,2,opt,name=name"`
	// Condition of the configCR status
	condv1alpha1.Condition `json:",inline" protobuf:"bytes,3,opt,name=condition"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories={sdc}

// ConfigSet is the Schema for the ConfigSet API
type ConfigSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec   ConfigSetSpec   `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	Status ConfigSetStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// ConfigSetList contains a list of ConfigSets
type ConfigSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []ConfigSet `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// Config type metadata.
var (
	ConfigSetKind = reflect.TypeOf(ConfigSet{}).Name()
)
