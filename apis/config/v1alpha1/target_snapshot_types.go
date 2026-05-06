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

package v1alpha1

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SensitiveConfigSpec defines the desired state of SensitiveConfig
type TargetSnapshotSpec struct {
	// Configs maps Config name to the last successfully applied resolved state.
	// Key is the Config CR name within the same namespace as the TargetSnapshot.
	// Each entry is independently encrypted — change detection via Payload.PlainHash
	// without decryption.
	Configs map[string]SensitiveConfigSpec `json:"configs" protobuf:"bytes,1,rep,name=configs"`

	// LastKnownGoodSchema identifies the last known good schema
	LastKnownGoodSchema *ConfigStatusLastKnownGoodSchema `json:"lastKnownGoodSchema,omitempty" protobuf:"bytes,2,opt,name=lastKnownGoodSchema"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:resource:categories={sdc}

// TargetSnapshot defines the Schema for the SensitiveConfig API
type TargetSnapshot struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec TargetSnapshotSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// SensitiveConfigList contains a list of SensitiveConfigs
type TargetSnapshotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []TargetSnapshot `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// SensitiveConfig type metadata.
var (
	TargetSnapshotKind = reflect.TypeOf(TargetSnapshot{}).Name()
)
