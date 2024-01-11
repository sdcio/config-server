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
)

// ConfigSetSpec defines the desired state of Config
type ConfigSetSpec struct {
	// Targets defines the targets on which this configSet applies
	Target Target `json:"target" yaml:"target"`
	// Lifecycle determines the lifecycle policies the resource e.g. delete is orphan or delete
	// will follow
	Lifecycle Lifecycle `json:"lifecycle,omitempty" yaml:"lifecycle,omitempty"`
	// Priority defines the priority of this config
	Priority int `json:"priority,omitempty" yaml:"priroity,omitempty"`
	// Config defines the configuration to be applied to a target device
	//+kubebuilder:pruning:PreserveUnknownFields
	Config []ConfigBlob `json:"config" yaml:"config"`
}

type Target struct {
	// TargetSelector defines the selector used to select the targets to which the config applies
	TargetSelector *metav1.LabelSelector `json:"targetSelector,omitempty" yaml:"targetSelector,omitempty"`
}

// ConfigSetStatus defines the observed state of Config
type ConfigSetStatus struct {
	// ConditionedStatus provides the status of the Readiness using conditions
	// if the condition is true the other attributes in the status are meaningful
	ConditionedStatus `json:",inline" yaml:",inline"`
	// Targets defines the status of the configSet resource on the respective target
	Targets []TargetStatus `json:"targets,omitempty" yaml:"targets,omitempty"`
}

type TargetStatus struct {
	Name string `json:"name" yaml:"name"`
	// right now we assume the namespace of the config and target are aligned
	//NameSpace string `json:"namespace" yaml:"namespace"`
	// Condition of the configCR status
	Condition `json:",inline" yaml:",inline"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

//	ConfigSet is the Schema for the ConfigSet API
//
// +k8s:openapi-gen=true
type ConfigSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ConfigSetSpec   `json:"spec,omitempty"`
	Status ConfigSetStatus `json:"status,omitempty"`
}

// ConfigSetList contains a list of ConfigSets
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ConfigSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ConfigSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ConfigSet{}, &ConfigSetList{})
}

// Config type metadata.
var (
	ConfigSetKind = reflect.TypeOf(ConfigSet{}).Name()
	//ConfigGroupKind        = schema.GroupKind{Group: GroupVersion.Group, Kind: ConfigKind}.String()
	//ConfigKindAPIVersion   = ConfigKind + "." + GroupVersion.String()
	//ConfigGroupVersionKind = GroupVersion.WithKind(ConfigKind)
)

