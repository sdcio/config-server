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
	cond "github.com/sdcio/config-server/apis/condition"
)

// Condition Types.
const (
	ConditionTypeConfigReady cond.ConditionType = "ConfigReady"
	ConditionTypeTargetReady cond.ConditionType = "TargetReady"
)

const (
	// Target relation conditions
	// ConditionTypeDiscoveryReady represents the resource discovery ready condition
	ConditionTypeTargetDiscoveryReady cond.ConditionType = "TargetDiscoveryReady"
	// ConditionTypeDatastoreReady represents the resource datastore ready condition
	ConditionTypeTargetDatastoreReady cond.ConditionType = "TargetDatastoreReady"
	// ConditionTypeConfigRecoveryReady represents the resource config recovery ready condition
	ConditionTypeTargetConfigRecoveryReady cond.ConditionType = "TargetConfigRecoveryReady"
	// ConditionTypeTargetConnectionReady represents the resource target ready condition
	ConditionTypeTargetConnectionReady cond.ConditionType = "TargetConnectionReady"

	//ConditionTypeConfigApply   cond.ConditionType = "ConfigApply"
	//ConditionTypeConfigConfirm cond.ConditionType = "ConfigConfirm"
	//ConditionTypeConfigCancel  cond.ConditionType = "ConfigCancel"

	//ConditionTypeSchemaServerReady cond.ConditionType = "SchemaServerReady"

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
