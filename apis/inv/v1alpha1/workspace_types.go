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

// WorkspaceSpec defines the desired state of Workspace
type WorkspaceSpec struct {
	Repository `json:",inline" protobuf:"bytes,1,opt,name=repository"`
}

// WorkspaceStatus defines the observed state of Workspace
type WorkspaceStatus struct {
	// ConditionedStatus provides the status of the Workspace using conditions
	condv1alpha1.ConditionedStatus `json:",inline" protobuf:"bytes,1,opt,name=conditionedStatus"`
	// +kubebuilder:validation:Enum=branch;tag;hash;
	// Kind defines the that the BranchOrTag string is a repository branch or a tag
	Kind *BranchTagKind `json:"kind,omitempty" protobuf:"bytes,2,opt,name=kind,casttype=BranchTagKind"`
	// DeployedRef is the reference that is deployed
	DeployedRef *string `json:"deployedRef,omitempty" protobuf:"bytes,3,opt,name=deployedRef"`
}

// +kubebuilder:object:root=true
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="URL",type="string",JSONPath=".spec.repoURL"
// +kubebuilder:printcolumn:name="REF",type="string",JSONPath=".ref"
// +kubebuilder:resource:categories={sdc,inv}
// Workspace is the Workspace for the Workspace API
// +k8s:openapi-gen=true
type Workspace struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec   WorkspaceSpec   `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	Status WorkspaceStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// +kubebuilder:object:root=true
// WorkspaceList contains a list of Workspaces
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type WorkspaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []Workspace `json:"items" protobuf:"bytes,2,rep,name=items"`
}

func init() {
	localSchemeBuilder.Register(&Workspace{}, &WorkspaceList{})
}

var (
	WorkspaceKind = reflect.TypeOf(Workspace{}).Name()
)
