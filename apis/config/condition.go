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
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
)

const (
	// ConditionTypeDiscoveryReady represents the resource discovery ready condition
	ConditionTypeDiscoveryReady condv1alpha1.ConditionType = "DiscoveryReady"
	// ConditionTypeDatastoreReady represents the resource datastore ready condition
	ConditionTypeDatastoreReady condv1alpha1.ConditionType = "DatastoreReady"
	// ConditionTypeConfigRecoveryReady represents the resource config recovery ready condition
	ConditionTypeConfigRecoveryReady condv1alpha1.ConditionType = "ConfigRecoveryReady"
	// ConditionTypeTargetConnectionReady represents the resource target ready condition
	ConditionTypeTargetConnectionReady condv1alpha1.ConditionType = "TargetConnectionReady"

	ConditionTypeConfigApply   condv1alpha1.ConditionType = "ConfigApply"
	ConditionTypeConfigConfirm condv1alpha1.ConditionType = "ConfigConfirm"
	ConditionTypeConfigCancel  condv1alpha1.ConditionType = "ConfigCancel"

	ConditionTypeSchemaServerReady condv1alpha1.ConditionType = "SchemaServerReady"
)

// A ConditionReason represents the reason a resource is in a condition.
type ConditionReason string

// Reasons a resource is ready or not
const (
	ConditionReasonReady          ConditionReason = "ready"
	ConditionReasonNotReady       ConditionReason = "notReady"
	ConditionReasonFailed         ConditionReason = "failed"
	ConditionReasonUnknown        ConditionReason = "unknown"
	ConditionReasonDeleting       ConditionReason = "deleting"
	ConditionReasonCreating       ConditionReason = "creating"
	ConditionReasonUpdating       ConditionReason = "updating"
	ConditionReasonTargetDeleted  ConditionReason = "target Deleted"
	ConditionReasonTargetNotReady ConditionReason = "target not ready"
	ConditionReasonTargetNotFound ConditionReason = "target not found"
)
