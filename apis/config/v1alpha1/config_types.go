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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// ConfigSpec defines the desired state of Config
type ConfigSpec struct {
	// Lifecycle determines the lifecycle policies the resource e.g. delete is orphan or delete
	// will follow
	Lifecycle Lifecycle `json:"lifecycle,omitempty" yaml:"lifecycle,omitempty"`
	// Priority defines the priority of this config
	Priority int `json:"priority,omitempty" yaml:"priroity,omitempty"`
	// Config defines the configuration to be applied to a target device
	//+kubebuilder:pruning:PreserveUnknownFields
	Config []ConfigBlob `json:"config" yaml:"config"`
}

type ConfigBlob struct {
	// Path defines the path relative to which the value is applicable
	Path string `json:"path,omitempty" yaml:"path,omitempty"`
	//+kubebuilder:pruning:PreserveUnknownFields
	Value runtime.RawExtension `json:"value" yaml:"value"`
}

// ConfigStatus defines the observed state of Config
type ConfigStatus struct {
	// ConditionedStatus provides the status of the Readiness using conditions
	// if the condition is true the other attributes in the status are meaningful
	ConditionedStatus `json:",inline" yaml:",inline"`
	// LastKnownGoodSchema identifies the last known good schema used to apply the config successfully
	LastKnownGoodSchema *ConfigStatusLastKnownGoodSchema `json:"lastKnownGoodSchema,omitempty" yaml:"lastKnownGoodSchema,omitempty"`
}

type ConfigStatusLastKnownGoodSchema struct {
	// Schema Type
	Type string `json:"type,omitempty" yaml:"type,omitempty"`
	// Schema Vendor
	Vendor string `json:"vendor,omitempty" yaml:"vendor,omitempty"`
	// Schema Version
	Version string `json:"version,omitempty" yaml:"version,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

//	Config is the Schema for the Config API
//
// +k8s:openapi-gen=true
type Config struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ConfigSpec   `json:"spec,omitempty"`
	Status ConfigStatus `json:"status,omitempty"`
}

// ConfigList contains a list of Configs
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Config `json:"items"`
}

/*
// Config type metadata.
var (
	ConfigKind             = reflect.TypeOf(Config{}).Name()
	ConfigGroupKind        = schema.GroupKind{Group: GroupVersion.Group, Kind: ConfigKind}.String()
	ConfigKindAPIVersion   = ConfigKind + "." + GroupVersion.String()
	ConfigGroupVersionKind = GroupVersion.WithKind(ConfigKind)
)
*/
