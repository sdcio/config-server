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

// ConfigSpec defines the desired state of Config
type ConfigSpec struct {
	// Lifecycle determines the lifecycle policies the resource e.g. delete is orphan or delete
	// will follow
	Lifecycle Lifecycle `json:"lifecycle,omitempty" protobuf:"bytes,1,opt,name=lifecycle"`
	// Priority defines the priority of this config
	Priority int64 `json:"priority,omitempty" protobuf:"bytes,2,opt,name=priority"`
	// Config defines the configuration to be applied to a target device
	//+kubebuilder:pruning:PreserveUnknownFields
	Config []ConfigBlob `json:"config" protobuf:"bytes,3,rep,name=config"`
}

type ConfigBlob struct {
	// Path defines the path relative to which the value is applicable
	Path string `json:"path,omitempty" protobuf:"bytes,1,opt,name=config"`
	//+kubebuilder:pruning:PreserveUnknownFields
	Value runtime.RawExtension `json:"value" protobuf:"bytes,2,opt,name=value"`
}

// ConfigStatus defines the observed state of Config
type ConfigStatus struct {
	// ConditionedStatus provides the status of the Readiness using conditions
	// if the condition is true the other attributes in the status are meaningful
	ConditionedStatus `json:",inline" protobuf:"bytes,1,opt,name=conditionedStatus"`
	// LastKnownGoodSchema identifies the last known good schema used to apply the config successfully
	LastKnownGoodSchema *ConfigStatusLastKnownGoodSchema `json:"lastKnownGoodSchema,omitempty" protobuf:"bytes,2,opt,name=lastKnownGoodSchema"`
	// AppliedConfig defines the config applied to the target
	AppliedConfig *ConfigSpec `json:"appliedConfig,omitempty" protobuf:"bytes,3,opt,name=appliedConfig"`
	// Deviations identify the configuration deviation based on the last applied config
	Deviations []Deviation `json:"deviations,omitempty" protobuf:"bytes,4,rep,name=deviations"`
}

type Deviation struct {
	// Path of the config this deviation belongs to
	Path string `json:"path,omitempty" protobuf:"bytes,1,opt,name=path"`
	// DesiredValue is the desired value of the config belonging to the path
	DesiredValue string `json:"desiredValue,omitempty" protobuf:"bytes,2,opt,name=desiredValue"`
	// ActualValue defines the actual value of the config belonging to the path
	// that is actually configured on the target
	ActualValue string `json:"actualValue,omitempty" protobuf:"bytes,3,opt,name=actualValue"`
	// Reason defines the reason of the deviation
	Reason string `json:"reason,omitempty" protobuf:"bytes,4,opt,name=reason"`
}

type ConfigStatusLastKnownGoodSchema struct {
	// Schema Type
	Type string `json:"type,omitempty" protobuf:"bytes,1,opt,name=type"`
	// Schema Vendor
	Vendor string `json:"vendor,omitempty" protobuf:"bytes,2,opt,name=vendor"`
	// Schema Version
	Version string `json:"version,omitempty" protobuf:"bytes,3,opt,name=version"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

//	Config is the Schema for the Config API
//
// +k8s:openapi-gen=true
type Config struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec   ConfigSpec   `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	Status ConfigStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// ConfigList contains a list of Configs
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []Config `json:"items" protobuf:"bytes,2,rep,name=items"`
}
