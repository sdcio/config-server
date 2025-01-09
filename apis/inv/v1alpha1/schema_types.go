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
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type BranchTagKind string

const (
	BranchTagKindTag    BranchTagKind = "tag"
	BranchTagKindBranch BranchTagKind = "branch"
)

// SchemaSpec defines the desired state of Schema
type SchemaSpec struct {
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="provider is immutable"
	// Provider specifies the provider of the schema.
	Provider string `json:"provider" protobuf:"bytes,1,opt,name=provider"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="version is immutable"
	// Version defines the version of the schema
	Version string `json:"version" protobuf:"bytes,2,opt,name=version"`
	// +kubebuilder:validation:MinItems:=1
	// +kubebuilder:validation:MaxItems:=10
	// Repositories define the repositories used for building the provider schema
	Repositories []*SchemaSpecRepository `json:"repositories" protobuf:"bytes,3,rep,name=repositories"`
}

type SchemaSpecRepository struct {
	// RepositoryURL specifies the base URL for a given repository
	RepositoryURL string `json:"repoURL" yaml:"repoURL" protobuf:"bytes,1,opt,name=repoURL"`
	// Credentials defines the name of the secret that holds the credentials to connect to the repo
	Credentials string `json:"credentials,omitempty" yaml:"credentials,omitempty" protobuf:"bytes,2,opt,name=credentials"`
	// Proxy defines the HTTP/HTTPS proxy to be used to download the models.
	Proxy SchemaSpecProxy `json:"proxy,omitempty" yaml:"proxy,omitempty" protobuf:"bytes,3,opt,name=proxy"`
	// +kubebuilder:validation:Enum=branch;tag;
	// +kubebuilder:default:=tag
	// Kind defines the that the BranchOrTag string is a repository branch or a tag
	Kind BranchTagKind `json:"kind" yaml:"kind" protobuf:"bytes,4,opt,name=kind,casttype=BranchTagKind"`
	// Ref defines the branch or tag of the repository corresponding to the
	// provider schema version
	Ref string `json:"ref" yaml:"ref" protobuf:"bytes,5,opt,name=ref"`
	// +kubebuilder:validation:MaxItems=10
	// Dirs defines the list of directories that identified the provider schema in src/dst pairs
	// relative within the repository
	Dirs []SrcDstPath `json:"dirs,omitempty" yaml:"dirs,omitempty" protobuf:"bytes,6,rep,name=dirs"`
	// Schema provides the details of which files must be used for the models and which files/directories
	// cana be excludes
	Schema SchemaSpecSchema `json:"schema" yaml:"schema" protobuf:"bytes,7,opt,name=schema"`
}

// SrcDstPath provide a src/dst pair for the loader to download the schema from a specific src
// in the repository to a given destination in the schema server
type SrcDstPath struct {
	// Src is the relative directory in the repository URL
	Src string `json:"src" yaml:"src" protobuf:"bytes,1,opt,name=src"`
	// Dst is the relative directory in the schema server
	Dst string `json:"dst" yaml:"dst" protobuf:"bytes,2,opt,name=dst"`
}

type SchemaSpecSchema struct {
	// +kubebuilder:validation:MaxItems=64
	// Models defines the list of files/directories to be used as a model
	Models []string `json:"models,omitempty" yaml:"models,omitempty" protobuf:"bytes,1,rep,name=models"`
	// +kubebuilder:validation:MaxItems=64
	// Excludes defines the list of files/directories to be excluded
	Includes []string `json:"includes,omitempty" yaml:"includes,omitempty" protobuf:"bytes,2,rep,name=includes"`
	// +kubebuilder:validation:MaxItems=64
	// Excludes defines the list of files/directories to be excluded
	Excludes []string `json:"excludes,omitempty" yaml:"excludes,omitempty" protobuf:"bytes,3,rep,name=excludes"`
}

type SchemaSpecProxy struct {
	// URL specifies the base URL of the HTTP/HTTPS proxy server.
	URL string `json:"URL,omitempty" yaml:"URL,omitempty" protobuf:"bytes,1,opt,name=URL"`
	// Credentials defines the name of the secret that holds the credentials to connect to the proxy server
	Credentials string `json:"credentials,omitempty" yaml:"credentials,omitempty" protobuf:"bytes,2,opt,name=credentials"`
}

// SchemaStatus defines the observed state of Schema
type SchemaStatus struct {
	// ConditionedStatus provides the status of the Schema using conditions
	condv1alpha1.ConditionedStatus `json:",inline" yaml:",inline" protobuf:"bytes,1,opt,name=conditionedStatus"`
}

// +kubebuilder:object:root=true
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="PROVIDER",type="string",JSONPath=".spec.provider"
// +kubebuilder:printcolumn:name="VERSION",type="string",JSONPath=".spec.version"
// +kubebuilder:printcolumn:name="URL",type="string",JSONPath=".spec.repositories[0].repoURL"
// +kubebuilder:printcolumn:name="REF",type="string",JSONPath=".spec.repositories[0].ref"
// +kubebuilder:resource:categories={sdc,inv}
// Schema is the Schema for the Schema API
// +k8s:openapi-gen=true
type Schema struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec   SchemaSpec   `json:"spec,omitempty" yaml:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	Status SchemaStatus `json:"status,omitempty" yaml:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// +kubebuilder:object:root=true
// SchemaList contains a list of Schemas
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type SchemaList struct {
	metav1.TypeMeta `json:",inline" yaml:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" yaml:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []Schema `json:"items" yaml:"items" protobuf:"bytes,2,rep,name=items"`
}

func init() {
	localSchemeBuilder.Register(&Schema{}, &SchemaList{})
}

var (
	SchemaKind             = reflect.TypeOf(Schema{}).Name()
	SchemaGroupKind        = schema.GroupKind{Group: SchemeGroupVersion.Group, Kind: SchemaKind}.String()
	SchemaKindAPIVersion   = SchemaKind + "." + SchemeGroupVersion.String()
	SchemaGroupVersionKind = SchemeGroupVersion.WithKind(SchemaKind)
)
