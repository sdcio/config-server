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
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
)

// GetCondition returns the condition based on the condition kind
func (r *Target) GetCondition(t condv1alpha1.ConditionType) condv1alpha1.Condition {
	return r.Status.GetCondition(t)
}

// SetConditions sets the conditions on the resource. it allows for 0, 1 or more conditions
// to be set at once
func (r *Target) SetConditions(c ...condv1alpha1.Condition) {
	r.Status.SetConditions(c...)
}

func (r *Target) SetOverallStatus() {
	ready := true
	msg := "target ready"
	if ready && !r.GetCondition(ConditionTypeDiscoveryReady).IsTrue() {
		ready = false
		msg = "discovery not ready"
	}
	if ready && !r.GetCondition(ConditionTypeDatastoreReady).IsTrue() {
		ready = false
		msg = "datastore not ready"
	}
	if ready && !r.GetCondition(ConditionTypeConfigReady).IsTrue() {
		ready = false
		msg = "config not ready"
	}
	if ready {
		r.Status.SetConditions(condv1alpha1.Ready())
	} else {
		r.Status.SetConditions(condv1alpha1.Failed(msg))
	}
}

func (r *Target) IsDatastoreReady() bool {
	return r.GetCondition(ConditionTypeDiscoveryReady).Status == metav1.ConditionTrue &&
		r.GetCondition(ConditionTypeDatastoreReady).Status == metav1.ConditionTrue
}

func (r *Target) IsReady() bool {
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
