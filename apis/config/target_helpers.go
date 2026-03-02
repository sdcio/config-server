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
	"github.com/sdcio/config-server/apis/condition"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetCondition returns the condition based on the condition kind
func (r *Target) GetConditions() []condition.Condition {
	return r.Status.GetConditions()
}

// GetCondition returns the condition based on the condition kind
func (r *Target) GetCondition(t condition.ConditionType) condition.Condition {
	return r.Status.GetCondition(t)
}

// SetConditions sets the conditions on the resource. it allows for 0, 1 or more conditions
// to be set at once
func (r *Target) SetConditions(c ...condition.Condition) {
	r.Status.SetConditions(c...)
}

func (r *Target) IsConditionReady() bool {
	return r.GetCondition(condition.ConditionTypeReady).Status == metav1.ConditionTrue
}

func (r *TargetStatus) GetDiscoveryInfo() DiscoveryInfo {
	if r.DiscoveryInfo != nil {
		return *r.DiscoveryInfo
	}
	return DiscoveryInfo{}
}

// BuildTarget returns a reource from a client Object a Spec/Status
func BuildTarget(meta metav1.ObjectMeta, spec TargetSpec) *Target {
	return &Target{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.Identifier(),
			Kind:       ConfigKind,
		},
		ObjectMeta: meta,
		Spec:       spec,
	}
}

func BuildEmptyTarget() *Target {
	return &Target{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.Identifier(),
			Kind:       TargetKind,
		},
	}
}

func (r *Target) IsReady() bool {
	return r.GetCondition(condition.ConditionTypeReady).Status == metav1.ConditionTrue &&
		r.GetCondition(ConditionTypeDiscoveryReady).Status == metav1.ConditionTrue &&
		r.GetCondition(ConditionTypeDatastoreReady).Status == metav1.ConditionTrue &&
		r.GetCondition(ConditionTypeTargetConnectionReady).Status == metav1.ConditionTrue
}
