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
	// Config related conditions, see below
	ConditionTypeConfigReady condv1alpha1.ConditionType = "ConfigReady"

	// Target relation conditions
	ConditionTypeTargetDiscoveryReady condv1alpha1.ConditionType = "TargetDiscoveryReady"
	// legacy - to be removed
	ConditionTypeTargetDatastoreReady      condv1alpha1.ConditionType = "TargetDatastoreReady"
	ConditionTypeTargetConfigRecoveryReady condv1alpha1.ConditionType = "TargetConfigRecoveryReady"
	ConditionTypeTargetConnectionReady     condv1alpha1.ConditionType = "TargetConnectionReady"
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
//     discovery -> schema -> configuration -> datastore -> recovery -> ready
const (
	TargetReasonDiscoveryNotReady    condv1alpha1.ConditionReason = "DiscoveryNotReady"
	TargetReasonSchemaNotReady       condv1alpha1.ConditionReason = "SchemaNotReady"
	TargetReasonInvalidConfiguration condv1alpha1.ConditionReason = "InvalidConfiguration"
	TargetReasonInitializing         condv1alpha1.ConditionReason = "Initializing"
	// Deprecated: the data-server runs in-process, so this reason is no longer
	// set. It stays exported for external consumers that still match on it.
	TargetReasonDataServerNotReady condv1alpha1.ConditionReason = "DataServerNotReady"
	TargetReasonDatastoreNotReady  condv1alpha1.ConditionReason = "DatastoreNotReady"
	TargetReasonDatastoreFailed    condv1alpha1.ConditionReason = "DatastoreFailed"
	TargetReasonRecovering         condv1alpha1.ConditionReason = "Recovering"
	TargetReasonRecoveryFailed     condv1alpha1.ConditionReason = "RecoveryFailed"
	TargetReasonDeleting           condv1alpha1.ConditionReason = "Deleting"
)

// A Config carries two conditions:
//
//   - Ready, for users. When False, the Reason names the first stage that
//     blocks the config:
//
//     TargetNotFound -> TargetDeleting -> TargetNotReady -> TargetRuntimeNotReady ->
//     Pending -> Failed | Unrecoverable
//
//     The target stages are written by the target config controller, the
//     outcome stages (Pending, Ready, Failed, Unrecoverable) follow ConfigReady.
//
//   - ConfigReady, for the controllers: the outcome of the last transaction
//     of this config. It is kept while the target is not ready, and drives the
//     next transaction: Failed configs are retried, Unrecoverable configs are
//     skipped until the spec changes (ObservedGeneration != metadata.generation).
const (
	ConfigReasonTargetNotFound        condv1alpha1.ConditionReason = "TargetNotFound"
	ConfigReasonTargetDeleting        condv1alpha1.ConditionReason = "TargetDeleting"
	ConfigReasonTargetNotReady        condv1alpha1.ConditionReason = "TargetNotReady"
	ConfigReasonTargetRuntimeNotReady condv1alpha1.ConditionReason = "TargetRuntimeNotReady"
	ConfigReasonPending               condv1alpha1.ConditionReason = "Pending"
)

// LegacyConfigConditionTypes are no longer written. The transactor field
// manager wrote them, so its next status apply on a Config removes them.
// Delete this once all deployments have been upgraded.
var LegacyConfigConditionTypes = []condv1alpha1.ConditionType{
	"TargetForConfigReady",
}

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

// ConfigReady returns the ConfigReady=True condition: the config of the
// given generation is applied on the target.
func ConfigReady(generation int64, msg string) condv1alpha1.Condition {
	return configCondition(generation, metav1.ConditionTrue, condv1alpha1.ConditionReasonReady, msg)
}

// ConfigFailed returns the ConfigReady=False condition for a recoverable
// failure: the config is retried in the next transaction.
func ConfigFailed(generation int64, msg string) condv1alpha1.Condition {
	return configCondition(generation, metav1.ConditionFalse, condv1alpha1.ConditionReasonFailed, msg)
}

// ConfigFailedUnrecoverable returns the ConfigReady=False condition for a
// failure that only a spec change can fix: the config is not retried as long
// as its generation equals ObservedGeneration (see Config.IsRecoverable).
func ConfigFailedUnrecoverable(generation int64, msg string) condv1alpha1.Condition {
	return configCondition(generation, metav1.ConditionFalse, condv1alpha1.ConditionReasonUnrecoverable, msg)
}

func configCondition(generation int64, status metav1.ConditionStatus, reason condv1alpha1.ConditionReason, msg string) condv1alpha1.Condition {
	c := condv1alpha1.NewCondition(ConditionTypeConfigReady, status, reason, msg)
	c.ObservedGeneration = generation
	return c
}

// ConfigNotReady returns the Ready=False condition of a Config for one of the
// target stages (ConfigReasonTarget*). generation is the Config generation
// the stage was observed for.
func ConfigNotReady(generation int64, reason condv1alpha1.ConditionReason, msg string) condv1alpha1.Condition {
	c := condv1alpha1.NewCondition(condv1alpha1.ConditionTypeReady, metav1.ConditionFalse, reason, msg)
	c.ObservedGeneration = generation
	return c
}

// ReadyFromConfigReady returns the Ready condition that follows a ConfigReady
// condition (the outcome of a transaction), with its observedGeneration.
func ReadyFromConfigReady(configReady condv1alpha1.Condition) condv1alpha1.Condition {
	var c condv1alpha1.Condition
	if configReady.IsTrue() {
		c = condv1alpha1.ReadyWithMsg(configReady.Message)
	} else {
		c = condv1alpha1.NewCondition(condv1alpha1.ConditionTypeReady, metav1.ConditionFalse,
			condv1alpha1.ConditionReason(configReady.Reason), configReady.Message)
	}
	c.ObservedGeneration = configReady.ObservedGeneration
	return c
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
