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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type SyncMode string

const (
	SyncMode_Unknown  SyncMode = "unknown"
	SyncMode_OnChange SyncMode = "onChange"
	SyncMode_Sample   SyncMode = "sample"
	SyncMode_Get      SyncMode = "get"
	SyncMode_Once     SyncMode = "once"
)

// +kubebuilder:validation:XValidation:rule="!has(oldSelf.sync) || has(self.sync)", message="sync is required once set"
// TargetSyncProfileSpec defines the desired state of TargetSyncProfile
type TargetSyncProfileSpec struct {
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="validate is immutable"
	// +kubebuilder:default:=true
	Validate bool `json:"validate,omitempty" yaml:"validate,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="buffer is immutable"
	// +kubebuilder:default:=0
	Buffer int64 `json:"buffer,omitempty" yaml:"buffer,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="workers is immutable"
	// +kubebuilder:default:=10
	Workers int64 `json:"workers,omitempty" yaml:"workers,omitempty"`
	// +kubebuilder:validation:MaxItems=10
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:XValidation:rule="oldSelf.all(x, x in self)",message="sync may only be added"
	Sync []TargetSyncProfileSync `json:"sync" yaml:"sync"`
}

// TargetSyncProfileSync defines the desired state of TargetSyncProfileSync
type TargetSyncProfileSync struct {
	Name string `json:"name" yaml:"name"`
	// +kubebuilder:validation:Enum=unknown;gnmi;netconf;noop;
	// +kubebuilder:default:="gnmi"
	Protocol Protocol `json:"protocol" yaml:"protocol"`
	// +kubebuilder:validation:MaxItems=10
	Paths []string `json:"paths" yaml:"paths"`
	//+kubebuilder:validation:Enum=unknown;onChange;sample;once;get;
	Mode SyncMode `json:"mode" yaml:"mode"`
	// +kubebuilder:validation:Enum=unknown;JSON;JSON_IETF;bytes;protobuf;ASCII;config;
	// +kubebuilder:default:="ASCII"
	Encoding Encoding `json:"encoding,omitempty" yaml:"encoding,omitempty"`
	// +kubebuilder:default:="60s"
	Interval metav1.Duration `json:"interval,omitempty" yaml:"interval,omitempty"`
}

// +kubebuilder:object:root=true
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:categories={sdc,inv}
// TargetSyncProfile is the Schema for the TargetSyncProfile API
// +k8s:openapi-gen=true
type TargetSyncProfile struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec TargetSyncProfileSpec `json:"spec,omitempty" yaml:"spec,omitempty"`
}

// +kubebuilder:object:root=true
// TargetSyncProfileList contains a list of TargetSyncProfiles
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type TargetSyncProfileList struct {
	metav1.TypeMeta `json:",inline" yaml:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Items           []TargetSyncProfile `json:"items" yaml:"items"`
}

func init() {
	localSchemeBuilder.Register(&TargetSyncProfile{}, &TargetSyncProfileList{})
}

var (
	TargetSyncProfileKind             = reflect.TypeOf(TargetSyncProfile{}).Name()
	TargetSyncProfileGroupKind        = schema.GroupKind{Group: SchemeGroupVersion.Group, Kind: TargetSyncProfileKind}.String()
	TargetSyncProfileKindAPIVersion   = TargetKind + "." + SchemeGroupVersion.String()
	TargetSyncProfileGroupVersionKind = SchemeGroupVersion.WithKind(TargetSyncProfileKind)
)
