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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true

// TargetRunningConfig is the Schema for the TargetRunning API
type TargetRunningConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	//+kubebuilder:pruning:PreserveUnknownFields
	Value runtime.RawExtension `json:"value" protobuf:"bytes,2,opt,name=value"`
}


// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true
// TargetRunningOptions is the Option Parametsrs for the TargetRunningOptions QueryParamters
type TargetRunningOptions struct {
	metav1.TypeMeta `json:",inline"`

	// Path filters the running config to a subtree
	Path string `json:"path,omitempty"`
	// Format controls output format
	Format string `json:"format,omitempty"`
}

