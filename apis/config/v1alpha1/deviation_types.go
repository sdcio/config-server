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

// DeviationSpec defines the desired state of Deviation
type DeviationSpec struct {
	// Deviations identify the configuration deviation based on the last applied config CR
	Deviations []ConfigDeviation `json:"deviations,omitempty" protobuf:"bytes,4,rep,name=deviations"`
}

type ConfigDeviation struct {
	// Path of the config this deviation belongs to
	Path string `json:"path,omitempty" protobuf:"bytes,1,opt,name=path"`
	// DesiredValue is the desired value of the config belonging to the path
	DesiredValue string `json:"desiredValue,omitempty" protobuf:"bytes,2,opt,name=desiredValue"`
	// CurrentValue defines the current value of the config belonging to the path
	// that is currently configured on the target
	CurrentValue string `json:"actualValue,omitempty" protobuf:"bytes,3,opt,name=actualValue"`
	// Reason defines the reason of the deviation
	Reason string `json:"reason,omitempty" protobuf:"bytes,4,opt,name=reason"`
}

// DeviationStatus defines the observed state of Deviation
type DeviationStatus struct {
	// ConditionedStatus provides the status of the Readiness using conditions
	// if the condition is true the other attributes in the status are meaningful
	condv1alpha1.ConditionedStatus `json:",inline" protobuf:"bytes,1,opt,name=conditionedStatus"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories={sdc}

// Deviation is the Schema for the Deviation API
type Deviation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec   DeviationSpec   `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	Status DeviationStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// DeviationList contains a list of Deviations
type DeviationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []Deviation `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// Deviation type metadata.
var (
	DeviationKind = reflect.TypeOf(Deviation{}).Name()
)
