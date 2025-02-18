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

type RolloutStrategy string

const (
	RolloutStrategy_NetworkWideTransaction RolloutStrategy = "networkWideTransaction"
)

// RolloutSpec defines the desired state of Rollout
type RolloutSpec struct {
	Repository `json:",inline" protobuf:"bytes,1,opt,name=repository"`

	Strategy RolloutStrategy `json:"strategy" protobuf:"bytes,2,opt,name=strategy"`

	SkipUnavailableTarget *bool `json:"skipUnavailableTarget,omitempty" protobuf:"bytes,3,opt,name=skipUnavailableTarget"`
}

// RolloutStatus defines the observed state of Rollout
type RolloutStatus struct {
	// ConditionedStatus provides the status of the Rollout using conditions
	condv1alpha1.ConditionedStatus `json:",inline" protobuf:"bytes,1,opt,name=conditionedStatus"`
	// Targets defines the status of the rollout on the respective target
	Targets []RolloutTargetStatus `json:"targets,omitempty" protobuf:"bytes,2,rep,name=targets"`
}

type RolloutTargetStatus struct {
	Name string `json:"name" protobuf:"bytes,1,opt,name=name"`
	// ConditionedStatus provides the status of the Rollout using conditions
	condv1alpha1.ConditionedStatus `json:",inline" protobuf:"bytes,2,opt,name=conditionedStatus"`
}

// +kubebuilder:object:root=true
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="URL",type="string",JSONPath=".spec.repoURL"
// +kubebuilder:printcolumn:name="REF",type="string",JSONPath=".spec.ref"
// +kubebuilder:resource:categories={sdc,inv}
// Rollout is the Rollout for the Rollout API
// +k8s:openapi-gen=true
type Rollout struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec   RolloutSpec   `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	Status RolloutStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// +kubebuilder:object:root=true
// RolloutList contains a list of Rollouts
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type RolloutList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []Rollout `json:"items" protobuf:"bytes,2,rep,name=items"`
}

func init() {
	localSchemeBuilder.Register(&Rollout{}, &RolloutList{})
}

var (
	RolloutKind = reflect.TypeOf(Rollout{}).Name()
)
