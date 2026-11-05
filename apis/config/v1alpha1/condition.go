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
	ConditionTypeTargetDiscoveryReady condv1alpha1.ConditionType = "TargetDiscoveryReady"
	// legacy - to be removed
	ConditionTypeTargetDatastoreReady condv1alpha1.ConditionType = "TargetDatastoreReady"
	ConditionTypeTargetConfigRecoveryReady condv1alpha1.ConditionType = "TargetConfigRecoveryReady"
	ConditionTypeTargetConnectionReady condv1alpha1.ConditionType = "TargetConnectionReady"
)

// Reasons a resource is ready or not
const (
	ConditionReasonCreating condv1alpha1.ConditionReason = "creating"
	ConditionReasonUpdating condv1alpha1.ConditionReason = "updating"
)

// A Target carries two conditions:
//
//   - TargetDiscoveryReady, owned by the discovery controller
//
//   - Ready, owned by the target datastore controller. It aggregates the
//     instantiation of the target; when False, the Reason names the first
//     stage of the pipeline that blocks:
//
//     discovery -> schema -> configuration -> dataserver -> datastore -> recovery -> ready
const (
	TargetReasonDiscoveryNotReady    condv1alpha1.ConditionReason = "DiscoveryNotReady"
	TargetReasonSchemaNotReady       condv1alpha1.ConditionReason = "SchemaNotReady"
	TargetReasonInvalidConfiguration condv1alpha1.ConditionReason = "InvalidConfiguration"
	TargetReasonInitializing         condv1alpha1.ConditionReason = "Initializing"
	TargetReasonDataServerNotReady   condv1alpha1.ConditionReason = "DataServerNotReady"
	TargetReasonDatastoreNotReady    condv1alpha1.ConditionReason = "DatastoreNotReady"
	TargetReasonDatastoreFailed      condv1alpha1.ConditionReason = "DatastoreFailed"
	TargetReasonRecovering           condv1alpha1.ConditionReason = "Recovering"
	TargetReasonRecoveryFailed       condv1alpha1.ConditionReason = "RecoveryFailed"
	TargetReasonDeleting             condv1alpha1.ConditionReason = "Deleting"
)

// AnnotationRecoveryRetry on a Target requests a new config recovery attempt
// after a non-recoverable recovery failure. Any new value triggers one retry:
//
//	kubectl annotate target <name> config.sdcio.dev/recovery-retry=$(date +%s) --overwrite
const AnnotationRecoveryRetry = "config.sdcio.dev/recovery-retry"

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

// ConfigFailedUnrecoverable returns a condition that indicates the config
// failed to apply for a reason considered unrecoverable without a spec
// change. msg is expected to already be the marshaled condv1alpha1.UnrecoverableMessage
// JSON payload, matching IsRecoverable's unmarshal expectations.
func ConfigFailedUnrecoverable(msg string) condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionTypeConfigReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonUnrecoverable),
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

// TargetForConfigFailed returns a condition that indicates the config
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

// Target Discovery related conditions

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

// Target DataStaore consitions

// TargetReady returns the Ready=True condition. msg may carry warnings
// (e.g. from config recovery).
func TargetReady(msg string) condv1alpha1.Condition {
	return condv1alpha1.ReadyWithMsg(msg)
}

// TargetNotReady returns the Ready=False condition with a pipeline reason.
// The message is bounded (condv1alpha1.MaxMessageBytes) at a rune boundary,
// with the cut marked, so long dataserver/device errors stay valid and readable.
func TargetNotReady(reason condv1alpha1.ConditionReason, msg string) condv1alpha1.Condition {
	return condv1alpha1.NewCondition(condv1alpha1.ConditionTypeReady, metav1.ConditionFalse, reason, msg)
}

// LegacyTargetConditionTypes are no longer written. They are removed once
// from existing Targets by the target datastore controller.
// Delete this (and the constants) once all deployments have been upgraded.
var LegacyTargetConditionTypes = []condv1alpha1.ConditionType{
	ConditionTypeTargetDatastoreReady,
	ConditionTypeTargetConnectionReady,
	ConditionTypeTargetConfigRecoveryReady,
}
