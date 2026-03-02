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

package config

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// RunningConfigSpec defines the desired state of RunningConfig
type RunningConfigSpec struct {
}

// RunningConfigStatus defines the observed state of RunningConfig
type RunningConfigStatus struct {
	//+kubebuilder:pruning:PreserveUnknownFields
	Value runtime.RawExtension `json:"value" protobuf:"bytes,2,opt,name=value"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories={sdc}

// RunningConfig defines the Schema for the RunningConfig API
type RunningConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec   RunningConfigSpec   `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	Status RunningConfigStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// RunningConfigList contains a list of RunningConfigs
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

type RunningConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []RunningConfig `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// RunningConfig type metadata.
var (
	RunningConfigKind = reflect.TypeOf(RunningConfig{}).Name()
)
