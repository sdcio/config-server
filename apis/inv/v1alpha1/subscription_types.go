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

type AdminState string

const (
	AdminState_ENABLED   AdminState = "enabled"
	AdminStaten_DISABLED AdminState = "disabled"
)

// SubscriptionSpec defines the desired Subscription of Subscription
type SubscriptionSpec struct {
	// Targets defines the targets on which this Subscription applies
	Target SubscriptionTarget `json:"target" protobuf:"bytes,1,opt,name=target"`
	// +kubebuilder:validation:Enum=unknown;gnmi;netconf;noop;
	// +kubebuilder:default:="gnmi"
	Protocol Protocol `json:"protocol" protobuf:"bytes,2,opt,name=protocol,casttype=Protocol"`
	// +kubebuilder:default:=57400
	// Port defines the port on which the scan runs
	Port uint32 `json:"port" protobuf:"varint,3,opt,name=port"`
	// +kubebuilder:validation:Enum=PROTO;ASCII;
	Encoding *Encoding `json:"encoding,omitempty" protobuf:"bytes,4,opt,name=encoding,casttype=Encoding"`
	// +kubebuilder:validation:MaxItems=128
	// +kubebuilder:validation:Optional
	Subscriptions []SubscriptionParameters `json:"subscriptions" protobuf:"bytes,5,rep,name=subscriptions"`
}

type SubscriptionTarget struct {
	// TargetSelector defines the selector used to select the targets to which the config applies
	TargetSelector *metav1.LabelSelector `json:"targetSelector,omitempty" protobuf:"bytes,1,opt,name=targetSelector"`
}

// SubscriptionSync defines the desired Subscription of SubscriptionSync
type SubscriptionParameters struct {
	// Name defines the name of the group of the Subscription to be collected
	Name string `json:"name" protobuf:"bytes,1,opt,name=name"`
	// Description details what the Subscription collection is about
	Description *string `json:"description,omitempty" protobuf:"bytes,2,opt,name=description"`
	// Labels can be defined as user defined data to provide extra context
	// +optional
	Labels map[string]string `json:"labels,omitempty" protobuf:"bytes,3,rep,name=labels"`
	// AdminState allows to disable the subscription
	// +kubebuilder:validation:Enum=enabled;disabled;
	// +kubebuilder:default:="enabled"
	AdminState *AdminState `json:"adminState,omitempty" protobuf:"bytes,4,opt,name=adminState,casttype=AdminState"`
	// +kubebuilder:validation:Enum=unknown;onChange;sample;
	// +kubebuilder:default:="sample"
	Mode SyncMode `json:"mode" protobuf:"bytes,5,opt,name=mode,casttype=SyncMode"`
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=duration
	// +kubebuilder:validation:Description="Duration should be a string representing a duration in seconds, minutes, or hours. E.g., '300s', '5m', '1h'."
	// +kubebuilder:validation:Enum="1s";"15s";"30s";"60s";
	Interval *metav1.Duration `json:"interval,omitempty" protobuf:"bytes,6,opt,name=interval"`
	// +kubebuilder:validation:MaxItems=128
	Paths []string `json:"paths" protobuf:"bytes,7,rep,name=paths"`
	// TODO Outputs define the outputs to which this information should go -> reight now we only support prometheus locally
	// this looks up another CR where they are defined with their respective secrets, etc
	//Outputs []string
}

type SubscriptionStatus struct {
	// ConditionedStatus provides the status of the Schema using conditions
	condv1alpha1.ConditionedStatus `json:",inline" protobuf:"bytes,1,opt,name=conditionedStatus"`
	// Targets defines the list of targets this resource applies to
	Targets []string `json:"targets,omitempty" protobuf:"bytes,2,rep,name=targets"`
}

// +kubebuilder:object:root=true
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="PROTOCOL",type="string",JSONPath=".spec.protocol"
// +kubebuilder:printcolumn:name="PORT",type="string",JSONPath=".spec.port"
// +kubebuilder:printcolumn:name="ENCODING",type="string",JSONPath=".spec.encoding"
// +kubebuilder:printcolumn:name="MODE",type="string",JSONPath=".spec.subscriptions[0].mode"
// +kubebuilder:printcolumn:name="INTERVAL",type="string",JSONPath=".spec.subscriptions[0].interval"
// +kubebuilder:resource:categories={sdc,inv}
// Subscription is the Schema for the Subscription API
// +k8s:openapi-gen=true
type Subscription struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec   SubscriptionSpec   `json:"spec,omitempty" yaml:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	Status SubscriptionStatus `json:"status,omitempty" yaml:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// +kubebuilder:object:root=true
// SubscriptionList contains a list of Subscriptions
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type SubscriptionList struct {
	metav1.TypeMeta `json:",inline" yaml:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" yaml:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []Subscription `json:"items" yaml:"items" protobuf:"bytes,2,rep,name=items"`
}

func init() {
	localSchemeBuilder.Register(&Subscription{}, &SubscriptionList{})
}

var (
	SubscriptionKind = reflect.TypeOf(Subscription{}).Name()
)
