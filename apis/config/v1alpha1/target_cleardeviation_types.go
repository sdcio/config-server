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
)

// TargetClearDeviationSpec defines the desired state of Target
type TargetClearDeviationSpec struct {
	// IncludeConfigs when true includes all existing configs in the transaction,
	// not just the ones referenced by Config.
	// +kubebuilder:default=false
	IncludeAllConfigs *bool `json:"includeAllConfigs,omitempty" protobuf:"bytes,2,opt,name=includeAllConfigs"`
	// Config defines the clear deviations configs to applied on the taget
	Config []TargetClearDeviationConfig `json:"config" protobuf:"bytes,3,rep,name=config"`
}

type TargetClearDeviationConfig struct {
	// Name of the config on which the paths should be cleared
	Name string `json:"name" protobuf:"bytes,1,opt,name=paths"`
	// Paths provide the path of the deviation to be cleared
	Paths []string `json:"paths" protobuf:"bytes,2,rep,name=paths"`
}

type TargetClearDeviationStatus struct {
	// Message is a global message for the transaction
	Message string `json:"message,omitempty" protobuf:"bytes,1,opt,name=message"`
	// Warnings are global warnings from the transaction
	Warnings []string `json:"warnings,omitempty" protobuf:"bytes,2,rep,name=warnings"`
	// Results holds per-config outcomes, keyed by intent name
	Results []TargetClearDeviationConfigResult `json:"results,omitempty" protobuf:"bytes,3,rep,name=results"`
}

type TargetClearDeviationConfigResult struct {
	// Name of the config on which the paths should be cleared
	Name string `json:"name" protobuf:"bytes,1,rep,name=paths"`
	// Success indicates whether the clear deviation was successful
	Success bool `json:"success"`
	// Message provides detail on the outcome
	Message string `json:"message,omitempty"`
	// Errors lists any errors for this specific config
	Errors []string `json:"errors,omitempty"`
	// Warnings lists any warnings for this specific config
	Warnings []string `json:"warnings,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// TargetClearDeviation is the Schema for the TargetClearDeviation API
// +k8s:openapi-gen=true
type TargetClearDeviation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec   *TargetClearDeviationSpec   `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	Status *TargetClearDeviationStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

var (
	TargetClearDeviationKind = reflect.TypeOf(TargetClearDeviation{}).Name()
)
