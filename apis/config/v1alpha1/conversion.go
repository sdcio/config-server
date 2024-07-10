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
	unsafe "unsafe"

	"github.com/sdcio/config-server/apis/condition"
	"github.com/sdcio/config-server/apis/condition/v1alpha1"
	conversion "k8s.io/apimachinery/pkg/conversion"
)

// Convert_v1alpha1_ConditionedStatus_To_condition_ConditionedStatus is hand made conversion function.
func Convert_v1alpha1_ConditionedStatus_To_condition_ConditionedStatus(in *v1alpha1.ConditionedStatus, out *condition.ConditionedStatus, s conversion.Scope) error {
	return autoConvert_v1alpha1_ConditionedStatus_To_condition_ConditionedStatus(in, out, s)
}

func autoConvert_v1alpha1_ConditionedStatus_To_condition_ConditionedStatus(in *v1alpha1.ConditionedStatus, out *condition.ConditionedStatus, _ conversion.Scope) error {
	out.Conditions = *(*[]condition.Condition)(unsafe.Pointer(&in.Conditions))
	return nil
}

// Convert_v1alpha1_ConditionedStatus_To_condition_ConditionedStatus is hand made conversion function.
func Convert_condition_ConditionedStatus_To_v1alpha1_ConditionedStatus(in *condition.ConditionedStatus, out *v1alpha1.ConditionedStatus, s conversion.Scope) error {
	return autoConvert_condition_ConditionedStatus_To_v1alpha1_ConditionedStatus(in, out, s)
}

func autoConvert_condition_ConditionedStatus_To_v1alpha1_ConditionedStatus(in *condition.ConditionedStatus, out *v1alpha1.ConditionedStatus, _ conversion.Scope) error {
	out.Conditions = *(*[]v1alpha1.Condition)(unsafe.Pointer(&in.Conditions))
	return nil
}

// Convert_condition_Condition_To_v1alpha1_Condition is hand made conversion function.
func Convert_condition_Condition_To_v1alpha1_Condition(in *condition.Condition, out *v1alpha1.Condition, s conversion.Scope) error {
	return autoConvert_condition_Condition_To_v1alpha1_Condition(in, out, s)
}

func autoConvert_condition_Condition_To_v1alpha1_Condition(in *condition.Condition, out *v1alpha1.Condition, _ conversion.Scope) error {
	out.Condition = in.Condition
	return nil
}


// Convert_TargetStatus_To_config_TargetStatus is hand made conversion function.
func Convert_v1alpha1_Condition_To_condition_Condition(in *v1alpha1.Condition, out *condition.Condition, s conversion.Scope) error {
	return autoConvert_v1alpha1_Condition_To_condition_Condition(in, out, s)
}

func autoConvert_v1alpha1_Condition_To_condition_Condition(in *v1alpha1.Condition, out *condition.Condition, _ conversion.Scope) error {
	out.Condition = in.Condition
	return nil
}