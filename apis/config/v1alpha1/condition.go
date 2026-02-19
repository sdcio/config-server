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
	ConditionTypeConfigReady condv1alpha1.ConditionType = "ConfigReady"
	ConditionTypeTargetReady condv1alpha1.ConditionType = "TargetReady"
)

// Reasons a resource is ready or not
const (
	//ConditionReasonDeleting       condv1alpha1.ConditionReason = "deleting"
	ConditionReasonCreating condv1alpha1.ConditionReason = "creating"
	ConditionReasonUpdating condv1alpha1.ConditionReason = "updating"
	//ConditionReasonTargetDeleted  condv1alpha1.ConditionReason = "target Deleted"
	ConditionReasonTargetNotReady condv1alpha1.ConditionReason = "target not ready"
	ConditionReasonTargetNotFound condv1alpha1.ConditionReason = "target not found"
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

// TargetReady return a condition that indicates
// the target became ready
func TargetReady(msg string) condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeTargetReady),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonReady),
		Message:            msg,
	}}
}

// ConfigFailed returns a condition that indicates the config
// is in failed condition due to a dependency
func TargetFailed(msg string) condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeTargetReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonFailed),
		Message:            msg,
	}}
}
