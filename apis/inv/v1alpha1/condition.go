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
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Condition Types.
const (
	// ConditionTypeDiscoveryReady represents the resource discovery ready condition
	ConditionTypeDiscoveryReady condv1alpha1.ConditionType = "DiscoveryReady"
	// ConditionTypeDatastoreReady represents the resource datastore ready condition
	ConditionTypeDatastoreReady condv1alpha1.ConditionType = "DatastoreReady"
	// ConditionTypeCfgReady represents the resource config ready condition
	ConditionTypeConfigReady condv1alpha1.ConditionType = "ConfigReady"
	// ConditionTypeTargetConnectionReady represents the resource target ready condition
	ConditionTypeTargetConnectionReady condv1alpha1.ConditionType = "TargetConnectionReady"
)

// A ConditionReason represents the reason a resource is in a condition.
type ConditionReason string

// Reasons a resource is ready or not
const (
	ConditionReasonNotReady       ConditionReason = "NotReady"
	ConditionReasonAction         ConditionReason = "Action"
	ConditionReasonLoading        ConditionReason = "Loading"
	ConditionReasonSchemaNotReady ConditionReason = "SchemaNotReady"
	ConditionReasonReApplyFailed  ConditionReason = "ReApplyConfigFailed"
)

// Action returns a condition that indicates the resource is in an
// action status.
func Action(msg string) condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(condv1alpha1.ConditionTypeReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(ConditionReasonAction),
		Message:            msg,
	}}
}

// NotReady returns a condition that indicates the resource is in an
// not ready status.
func NotReady(msg string) condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(condv1alpha1.ConditionTypeReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(ConditionReasonNotReady),
		Message:            msg,
	}}
}

// Loading returns a condition that indicates the resource
// is loading.
func Loading() condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(condv1alpha1.ConditionTypeReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(ConditionReasonLoading),
		Message:            "loading",
	}}
}

// DatastoreReady indicates the datastire is ready
func DatastoreReady() condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeDatastoreReady),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonReady),
	}}
}

// DatastoreFailed returns a condition that indicates the datastore
// failed to get reconciled.
func DatastoreFailed(msg string) condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeDatastoreReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonFailed),
		Message:            msg,
	}}
}

// DatastoreSchemaNotReady returns a condition that indicates the schema
// of the datastore is not ready.
func DatastoreSchemaNotReady(msg string) condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeDatastoreReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(ConditionReasonSchemaNotReady),
		Message:            msg,
	}}
}

// ConfigReady return a condition that indicates the config
// get re-applied when the target became ready
func ConfigReady() condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeConfigReady),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonReady),
	}}
}

// ConfigFailed returns a condition that indicates the config
// is in failed condition due to a dependency
func ConfigFailed(msg string) condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeConfigReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonFailed),
		Message:            msg,
	}}
}

// ConfigReApplyFailed returns a condition that indicates the config
// we we reapplied to the target
func ConfigReApplyFailed(msg string) condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeConfigReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(ConditionReasonReApplyFailed),
		Message:            msg,
	}}
}

// DiscoveryReady return a condition that indicates the discovery
// is ready
func DiscoveryReady() condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeDiscoveryReady),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonReady),
	}}
}

// DiscoveryFailed returns a condition that indicates the discovery
// is in failed condition
func DiscoveryFailed(msg string) condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeDiscoveryReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonFailed),
		Message:            msg,
	}}
}

// TargetConnectionReady return a condition that indicates the target connection
// is ready
func TargetConnectionReady() condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeTargetConnectionReady),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonReady),
	}}
}

// TargetConnectionFailed returns a condition that indicates the target connection
// is in failed condition
func TargetConnectionFailed(msg string) condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeTargetConnectionReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonFailed),
		Message:            msg,
	}}
}
