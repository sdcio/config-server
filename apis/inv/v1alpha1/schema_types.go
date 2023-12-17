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
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type BranchTagKind string

const (
	BranchTagKindTag    BranchTagKind = "tag"
	BranchTagKindBranch BranchTagKind = "branch"
)

// SchemaSpec defines the desired state of Schema
type SchemaSpec struct {
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="url is immutable"
	// URL specifies the base URL for a given repository
	RepositoryURL string `json:"repoURL" yaml:"repoURL"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="provider is immutable"
	// Provider specifies the provider of the schema.
	Provider string `json:"provider" yaml:"provider"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="version is immutable"
	// Version defines the version of the schema
	Version string `json:"version" yaml:"version"`
	// +kubebuilder:validation:Enum=branch;tag;
	// Kind defines the that the BranchOrTag string is a repository branch or a tag
	Kind BranchTagKind `json:"kind" yaml:"kind"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="ref is immutable"
	// Ref defines the branch or tag of the repository corresponding to the
	// provider schema version
	Ref string `json:"ref" yaml:"ref"`
	// +kubebuilder:validation:MaxItems=10
	// +kubebuilder:validation:XValidation:rule="oldSelf.all(x, x in self)",message="dirs is immutable"
	// Dirs defines the list of directories that identified the provider schema in src/dst pairs
	// relative within the repository
	Dirs []SrcDstPath `json:"dirs" yaml:"dirs"`

	// Schema provides the details of which files must be used for the models and which files/directories
	// cana be excludes
	Schema SchemaSpecSchema `json:"schema" yaml:"schema"`
	
}

// SrcDstPath provide a src/dst pair for the loader to download the schema from a specific src
// in the repository to a given destination in the schema server
type SrcDstPath struct {
	// Src is the relative directory in the repository URL
	Src string `json:"src" yaml:"src"`
	// Dst is the relative directory in the schema server
	Dst string `json:"dst" yaml:"dst"`
}

type SchemaSpecSchema struct {
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:XValidation:rule="oldSelf.all(x, x in self)",message="models is immutable"
	// Models defines the list of files/directories to be used as a model
	Models []string `json:"models" yaml:"models"`
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:XValidation:rule="oldSelf.all(x, x in self)",message="includes is immutable"
	// Excludes defines the list of files/directories to be excluded
	Includes []string `json:"includes" yaml:"includes"`
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:XValidation:rule="oldSelf.all(x, x in self)",message="excludes is immutable"
	// Excludes defines the list of files/directories to be excluded
	Excludes []string `json:"excludes" yaml:"excludes"`
}

// SchemaStatus defines the observed state of Schema
type SchemaStatus struct {
	// ConditionedStatus provides the status of the Schema using conditions
	// 2 conditions are used:
	// - a condition for the reconcilation status
	// - a condition for the ready status
	// if both are true the other attributes in the status are meaningful
	ConditionedStatus `json:",inline" yaml:",inline"`
}

// +kubebuilder:object:root=true
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="URL",type="string",JSONPath=".spec.repoURL"
// +kubebuilder:printcolumn:name="REF",type="string",JSONPath=".spec.ref"
// +kubebuilder:printcolumn:name="PROVIDER",type="string",JSONPath=".spec.provider"
// +kubebuilder:printcolumn:name="VERSION",type="string",JSONPath=".spec.version"
// +kubebuilder:resource:categories={sdc,inv}
// Schema is the Schema for the Schema API
// +k8s:openapi-gen=true
type Schema struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec   SchemaSpec   `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status SchemaStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

// +kubebuilder:object:root=true
// SchemaList contains a list of Schemas
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type SchemaList struct {
	metav1.TypeMeta `json:",inline" yaml:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Items           []Schema `json:"items" yaml:"items"`
}

func init() {
	SchemeBuilder.Register(&Schema{}, &SchemaList{})
}

var (
	SchemaKind             = reflect.TypeOf(Schema{}).Name()
	SchemaGroupKind        = schema.GroupKind{Group: SchemeGroupVersion.Group, Kind: SchemaKind}.String()
	SchemaKindAPIVersion   = SchemaKind + "." + SchemeGroupVersion.String()
	SchemaGroupVersionKind = SchemeGroupVersion.WithKind(SchemaKind)
)
