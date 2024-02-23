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
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetCondition returns the condition based on the condition kind
func (r *Target) GetCondition(t ConditionType) Condition {
	return r.Status.GetCondition(t)
}

// SetConditions sets the conditions on the resource. it allows for 0, 1 or more conditions
// to be set at once
func (r *Target) SetConditions(c ...Condition) {
	r.Status.SetConditions(c...)
}

func (r *Target) SetOverallStatus() {
	ready := true
	msg := "target ready"
	condition := r.GetCondition(ConditionTypeDiscoveryReady)
	if ready && condition.Status == metav1.ConditionFalse {
		ready = false
		msg = "discovery not ready"
	}
	condition = r.GetCondition(ConditionTypeDatastoreReady)
	if ready && condition.Status == metav1.ConditionFalse {
		ready = false
		msg = "datastore not ready"
	}
	condition = r.GetCondition(ConditionTypeConfigReady)
	if ready && condition.Status == metav1.ConditionFalse {
		ready = false
		msg = "config not ready"
	}
	if ready {
		r.Status.SetConditions(Ready())
	} else {
		r.Status.SetConditions(Failed(msg))
	}
}

func (r *Target) IsDatastoreReady() bool {
	return r.GetCondition(ConditionTypeDiscoveryReady).Status == metav1.ConditionTrue &&
		r.GetCondition(ConditionTypeDatastoreReady).Status == metav1.ConditionTrue
}

func (r *Target) IsConfigReady() bool {
	return r.GetCondition(ConditionTypeDiscoveryReady).Status == metav1.ConditionTrue &&
		r.GetCondition(ConditionTypeDatastoreReady).Status == metav1.ConditionTrue &&
		r.GetCondition(ConditionTypeConfigReady).Status == metav1.ConditionTrue
}

func (r *Target) NotReadyReason() string {
	discoveryReadyCondition := r.GetCondition(ConditionTypeDiscoveryReady)
	datastoreReadyCondition := r.GetCondition(ConditionTypeDatastoreReady)
	return fmt.Sprintf("ready: %v, reason: %s, msg: %s dsready: %v, reason: %s, msg: %s",
		discoveryReadyCondition.Status,
		discoveryReadyCondition.Reason,
		discoveryReadyCondition.Message,
		datastoreReadyCondition.Status,
		datastoreReadyCondition.Reason,
		datastoreReadyCondition.Message,
	)
}

func (r *TargetList) GetItems() []client.Object {
	objs := []client.Object{}
	for _, r := range r.Items {
		objs = append(objs, &r)
	}
	return objs
}

// BuildTarget returns a Target from a client Object a crName and
// an Target Spec/Status
func BuildTarget(meta metav1.ObjectMeta, spec TargetSpec, status TargetStatus) *Target {
	return &Target{
		TypeMeta: metav1.TypeMeta{
			APIVersion: localSchemeBuilder.GroupVersion.Identifier(),
			Kind:       TargetKind,
		},
		ObjectMeta: meta,
		Spec:       spec,
		Status:     status,
	}
}
