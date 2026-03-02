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
	// Config related conditions
	ConditionTypeConfigReady          condv1alpha1.ConditionType = "ConfigReady"
	ConditionTypeTargetForConfigReady condv1alpha1.ConditionType = "TargetForConfigReady"

	// Target relation conditions
	// ConditionTypeDiscoveryReady represents the resource discovery ready condition
	ConditionTypeTargetDiscoveryReady condv1alpha1.ConditionType = "TargetDiscoveryReady"
	// ConditionTypeDatastoreReady represents the resource datastore ready condition
	ConditionTypeTargetDatastoreReady condv1alpha1.ConditionType = "TargetDatastoreReady"
	// ConditionTypeConfigRecoveryReady represents the resource config recovery ready condition
	ConditionTypeTargetConfigRecoveryReady condv1alpha1.ConditionType = "TargetConfigRecoveryReady"
	// ConditionTypeTargetConnectionReady represents the resource target ready condition
	ConditionTypeTargetConnectionReady condv1alpha1.ConditionType = "TargetConnectionReady"
)

// Reasons a resource is ready or not
const (
	//ConditionReasonDeleting       condv1alpha1.ConditionReason = "deleting"
	ConditionReasonCreating condv1alpha1.ConditionReason = "creating"
	ConditionReasonUpdating condv1alpha1.ConditionReason = "updating"
	//ConditionReasonTargetDeleted  condv1alpha1.ConditionReason = "target Deleted"
	//ConditionReasonTargetNotReady condv1alpha1.ConditionReason = "target not ready"
	//ConditionReasonTargetNotFound condv1alpha1.ConditionReason = "target not found"
)

// Creating returns a condition that indicates a create transaction
// is ongoing
func Creating() condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeConfigReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(ConditionReasonCreating),
		Message:            "creating",
	}}
}

// Updating returns a condition that indicates a update transaction
// is ongoing
func Updating() condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeConfigReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(ConditionReasonUpdating),
		Message:            "updating",
	}}
}

// ConfigReady return a condition that indicates the config
// get re-applied when the target became ready
func ConfigReady(msg string) condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeConfigReady),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonReady),
		Message:            msg,
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

// TargetForConfigReady return a condition that indicates
// the target became ready
func TargetForConfigReady(msg string) condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeTargetForConfigReady),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonReady),
		Message:            msg,
	}}
}

// ConfigFailed returns a condition that indicates the config
// is in failed condition due to a dependency
func TargetForConfigFailed(msg string) condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeTargetForConfigReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonFailed),
		Message:            msg,
	}}
}

// Target related conditions

// TargetDatastoreReady indicates the datastire is ready
func TargetDatastoreReady() condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeTargetDatastoreReady),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonReady),
	}}
}

// DatastoreFailed returns a condition that indicates the datastore
// failed to get reconciled.
func TargetDatastoreFailed(msg string) condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeTargetDatastoreReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonFailed),
		Message:            msg,
	}}
}

// TargetDatastoreSchemaNotReady returns a condition that indicates the schema
// of the datastore is not ready.
func TargetDatastoreSchemaNotReady(msg string) condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeTargetDatastoreReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string("schema not ready"),
		Message:            msg,
	}}
}

// TargetConfigRecoveryReady return a condition that indicates the config
// get re-applied when the target became ready
func TargetConfigRecoveryReady(msg string) condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeTargetConfigRecoveryReady),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonReady),
		Message:            msg,
	}}
}

// TargetConfigRecoveryFailed returns a condition that indicates the config
// is in failed condition due to a dependency
func TargetConfigRecoveryFailed(msg string) condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeTargetConfigRecoveryReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonFailed),
		Message:            msg,
	}}
}

// TargetConfigRecoveryReApplyFailed returns a condition that indicates the config
// we we reapplied to the target
func TargetConfigRecoveryReApplyFailed(msg string) condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeTargetConfigRecoveryReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string("recovery config transaction failed"),
		Message:            msg,
	}}
}

// TargetDiscoveryReady return a condition that indicates the discovery
// is ready
func TargetDiscoveryReady() condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeTargetDiscoveryReady),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonReady),
	}}
}

// TargetDiscoveryFailed returns a condition that indicates the discovery
// is in failed condition
func TargetDiscoveryFailed(msg string) condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeTargetDiscoveryReady),
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
