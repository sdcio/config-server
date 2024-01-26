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
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// A ConditionType represents a condition type for a given KRM resource
type ConditionType string

// Condition Types.
const (
	// ConditionTypeReady represents the resource ready condition
	ConditionTypeReady ConditionType = "Ready"
	// ConditionTypeTargetReady represents the resource target ready condition
	ConditionTypeTargetReady ConditionType = "Ready"
)

// A ConditionReason represents the reason a resource is in a condition.
type ConditionReason string

// Reasons a resource is ready or not
const (
	ConditionReasonReady         ConditionReason = "Ready"
	ConditionReasonNotReady      ConditionReason = "NotReady"
	ConditionReasonFailed        ConditionReason = "Failed"
	ConditionReasonUnknown       ConditionReason = "Unknown"
	ConditionReasonTargetDeleted ConditionReason = "Target Deleted"
	ConditionReasonDeleting      ConditionReason = "Deleting"
	ConditionReasonCreating      ConditionReason = "Creating"
	ConditionReasonUpdating      ConditionReason = "Updating"
)

type Condition struct {
	metav1.Condition `json:",inline" protobuf:"bytes,1,opt,name=condition"`
}

// Equal returns true if the condition is identical to the supplied condition,
// ignoring the LastTransitionTime.
func (c Condition) Equal(other Condition) bool {
	return c.Type == other.Type &&
		c.Status == other.Status &&
		c.Reason == other.Reason &&
		c.Message == other.Message
}

// WithMessage returns a condition by adding the provided message to existing
// condition.
func (c Condition) WithMessage(msg string) Condition {
	c.Message = msg
	return c
}

// A ConditionedStatus reflects the observed status of a resource. Only
// one condition of each type may exist.
type ConditionedStatus struct {
	// Conditions of the resource.
	// +optional
	Conditions []Condition `json:"conditions,omitempty" protobuf:"bytes,1,rep,name=conditions"`
}

// NewConditionedStatus returns a stat with the supplied conditions set.
func NewConditionedStatus(c ...Condition) *ConditionedStatus {
	r := &ConditionedStatus{}
	r.SetConditions(c...)
	return r
}

// GetCondition returns the condition for the given ConditionKind if exists,
// otherwise returns nil
func (r *ConditionedStatus) GetCondition(t ConditionType) Condition {
	for _, c := range r.Conditions {
		if c.Type == string(t) {
			return c
		}
	}
	return Condition{metav1.Condition{Type: string(t), Status: metav1.ConditionFalse}}
}

// SetConditions sets the supplied conditions, replacing any existing conditions
// of the same type. This is a no-op if all supplied conditions are identical,
// ignoring the last transition time, to those already set.
func (r *ConditionedStatus) SetConditions(c ...Condition) {
	for _, new := range c {
		exists := false
		for i, existing := range r.Conditions {
			if existing.Type != new.Type {
				continue
			}

			if existing.Equal(new) {
				exists = true
				continue
			}

			r.Conditions[i] = new
			exists = true
		}
		if !exists {
			r.Conditions = append(r.Conditions, new)
		}
	}
}

// Equal returns true if the status is identical to the supplied status,
// ignoring the LastTransitionTimes and order of statuses.
func (r *ConditionedStatus) Equal(other *ConditionedStatus) bool {
	if r == nil || other == nil {
		return r == nil && other == nil
	}

	if len(other.Conditions) != len(r.Conditions) {
		return false
	}

	sc := make([]Condition, len(r.Conditions))
	copy(sc, r.Conditions)

	oc := make([]Condition, len(other.Conditions))
	copy(oc, other.Conditions)

	// We should not have more than one condition of each type.
	sort.Slice(sc, func(i, j int) bool { return sc[i].Type < sc[j].Type })
	sort.Slice(oc, func(i, j int) bool { return oc[i].Type < oc[j].Type })

	for i := range sc {
		if !sc[i].Equal(oc[i]) {
			return false
		}
	}
	return true
}

// Ready returns a condition that indicates the resource is
// ready for use.
func Ready() Condition {
	return Condition{metav1.Condition{
		Type:               string(ConditionTypeReady),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             string(ConditionReasonReady),
	}}
}

// Unknown returns a condition that indicates the resource is in an
// unknown status.
func Unknown() Condition {
	return Condition{metav1.Condition{
		Type:               string(ConditionTypeReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(ConditionReasonUnknown),
	}}
}

// Failed returns a condition that indicates the resource
// failed to get reconciled.
func Failed(msg string) Condition {
	return Condition{metav1.Condition{
		Type:               string(ConditionTypeReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(ConditionReasonFailed),
		Message:            msg,
	}}
}

// TargetDeleted returns a condition that indicates the resource
// target is deleted
func TargetDeleted() Condition {
	return Condition{metav1.Condition{
		Type:               string(ConditionTypeTargetReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(ConditionReasonTargetDeleted),
	}}
}

// TargetReady returns a condition that indicates the resource
// target is ready.
func TargetReady() Condition {
	return Condition{metav1.Condition{
		Type:               string(ConditionTypeTargetReady),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             string(ConditionReasonReady),
	}}
}

// TargetNotReady returns a condition that indicates the resource
// target is NOT ready.
func TargetNotReady(msg string) Condition {
	return Condition{metav1.Condition{
		Type:               string(ConditionTypeTargetReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(ConditionReasonNotReady),
		Message:            msg,
	}}
}

// Creating returns a condition that indicates a create transaction
// is ongoing
func Creating() Condition {
	return Condition{metav1.Condition{
		Type:               string(ConditionTypeTargetReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(ConditionReasonCreating),
	}}
}

// Creating returns a condition that indicates a delete transaction
// is ongoing
func Deleting() Condition {
	return Condition{metav1.Condition{
		Type:               string(ConditionTypeTargetReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(ConditionReasonDeleting),
	}}
}

// Creating returns a condition that indicates a update transaction
// is ongoing
func Updating() Condition {
	return Condition{metav1.Condition{
		Type:               string(ConditionTypeTargetReady),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(ConditionReasonUpdating),
	}}
}