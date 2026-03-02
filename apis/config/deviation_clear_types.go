/*
Copyright 2026 Nokia.

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

	"github.com/sdcio/config-server/apis/condition"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DeviationClearType string

const (
	DeviationClearType_TARGET     DeviationClearType = "target"
	DeviationClearType_CONFIG     DeviationClearType = "config"
	DeviationClearType_ALL_CONFIG DeviationClearType = "allConfig"
	DeviationClearType_ALL        DeviationClearType = "all"
)

func (r DeviationClearType) String() string {
	switch r {
	case DeviationClearType_TARGET:
		return "target"
	case DeviationClearType_CONFIG:
		return "config"
	case DeviationClearType_ALL_CONFIG:
		return "allConfig"
	case DeviationClearType_ALL:
		return "all"
	default:
		return "unknown"
	}
}

// DeviationSpec defines the desired state of Deviation
type DeviationClearSpec struct {
	Items []DeviationClearItem `json:"items" protobuf:"bytes,1,rep,name=items"`
}

type DeviationClearItem struct {
	Type DeviationClearType `json:"type" protobuf:"bytes,1,opt,name=type"`
	// Name of the config - mandatory if the type is config - not applicable for the other types
	ConfigName *string `json:"configName,omitempty" protobuf:"bytes,2,opt,name=configName"`
	// Paths of the respective type that should be cleared
	Paths []string `json:"paths" protobuf:"bytes,3,rep,name=paths"`
}

// DeviationStatus defines the observed state of Deviationgit
type DeviationClearStatus struct {
	// ConditionedStatus provides the status of the Readiness using conditions
	// if the condition is true the other attributes in the status are meaningful
	condition.ConditionedStatus `json:",inline" protobuf:"bytes,1,opt,name=conditionedStatus"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories={sdc}

// DeviationClear is the Schema for the DeviationClear API
type DeviationClear struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec   DeviationClearSpec   `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	Status DeviationClearStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// DeviationClearList contains a list of DeviationClears
type DeviationClearList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []Deviation `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// DeviationClearKind type metadata.
var (
	DeviationClearKind = reflect.TypeOf(DeviationClear{}).Name()
)
