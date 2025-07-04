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

/*
import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// UnManagedConfigSpec defines the desired state of UnManagedConfig
type UnManagedConfigSpec struct {
}

// UnManagedConfigStatus defines the observed state of UnManagedConfig
type UnManagedConfigStatus struct {
	// Deviations identify the configuration deviation based on the last applied config
	Deviations []Deviation `json:"deviations,omitempty" protobuf:"bytes,4,rep,name=deviations"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories={sdc}

// UnManagedConfig is the Schema for the UnManagedConfig API
type UnManagedConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec   UnManagedConfigSpec   `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	Status UnManagedConfigStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// UnManagedConfigList contains a list of UnManagedConfigs
type UnManagedConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []UnManagedConfig `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// UnManagedConfig type metadata.
var (
	UnManagedConfigKind = reflect.TypeOf(UnManagedConfig{}).Name()
)
*/
