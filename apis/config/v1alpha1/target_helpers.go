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
	"strings"

	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func (r *Target) IsConditionReady() bool {
	return r.GetCondition(condv1alpha1.ConditionTypeReady).Status == metav1.ConditionTrue
}

func (r *Target) GetOwnerReference() metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: r.APIVersion,
		Kind:       r.Kind,
		Name:       r.Name,
		UID:        r.UID,
		Controller: ptr.To(true),
	}
}

func (r *Target) GetNamespacedName() types.NamespacedName {
	return types.NamespacedName{Namespace: r.Namespace, Name: r.Name}
}

func (r *Target) SetOverallStatus(target *Target) {
	ready := true
	msg := "target ready"
	if ready && !target.GetCondition(ConditionTypeTargetDiscoveryReady).IsTrue() {
		ready = false
		msg = "discovery not ready" // target.GetCondition(ConditionTypeDiscoveryReady).Message)
	}
	if ready && !target.GetCondition(ConditionTypeTargetDatastoreReady).IsTrue() {
		ready = false
		msg = "datastore not ready" // target.GetCondition(ConditionTypeDatastoreReady).Message)
	}
	if ready && !target.GetCondition(ConditionTypeTargetConfigRecoveryReady).IsTrue() {
		ready = false
		msg = "config not ready" //, target.GetCondition(ConditionTypeConfigReady).Message)
	}
	if ready && !target.GetCondition(ConditionTypeTargetConnectionReady).IsTrue() {
		ready = false
		msg = "datastore connection not ready" // target.GetCondition(ConditionTypeTargetConnectionReady).Message)
	}
	if ready {
		r.Status.SetConditions(condv1alpha1.Ready())
	} else {
		r.Status.SetConditions(condv1alpha1.Failed(msg))
	}
}

func GetOverallStatus(target *Target) condv1alpha1.Condition {
	ready := true
	msg := "target ready"
	if ready && !target.GetCondition(ConditionTypeTargetDiscoveryReady).IsTrue() {
		ready = false
		msg = "discovery not ready" // target.GetCondition(ConditionTypeDiscoveryReady).Message)
	}
	if ready && !target.GetCondition(ConditionTypeTargetDatastoreReady).IsTrue() {
		ready = false
		msg = "datastore not ready" // target.GetCondition(ConditionTypeDatastoreReady).Message)
	}
	if ready && !target.GetCondition(ConditionTypeTargetConfigRecoveryReady).IsTrue() {
		ready = false
		msg = "config not ready" //, target.GetCondition(ConditionTypeConfigReady).Message)
	}
	if ready && !target.GetCondition(ConditionTypeTargetConnectionReady).IsTrue() {
		ready = false
		msg = "datastore connection not ready" // target.GetCondition(ConditionTypeTargetConnectionReady).Message)
	}
	if !ready {
		return condv1alpha1.Failed(msg)
	}
	return condv1alpha1.Ready()
}

func (r *Target) IsDatastoreReady() bool {
	return r.GetCondition(ConditionTypeTargetDiscoveryReady).Status == metav1.ConditionTrue &&
		r.GetCondition(ConditionTypeTargetDatastoreReady).Status == metav1.ConditionTrue &&
		r.GetCondition(ConditionTypeTargetConnectionReady).Status == metav1.ConditionTrue
}

func (r *Target) IsReady() bool {
	return r.GetCondition(condv1alpha1.ConditionTypeReady).Status == metav1.ConditionTrue &&
		r.GetCondition(ConditionTypeTargetDiscoveryReady).Status == metav1.ConditionTrue &&
		r.GetCondition(ConditionTypeTargetDatastoreReady).Status == metav1.ConditionTrue &&
		r.GetCondition(ConditionTypeTargetConnectionReady).Status == metav1.ConditionTrue
}

func (r *Target) NotReadyReason() string {
	reasons := []string{}

	check := func(name string, cond condv1alpha1.Condition) {
		if cond.Status != metav1.ConditionTrue {
			reasons = append(reasons,
				fmt.Sprintf("%s: %s (Reason: %s, Msg: %s)",
					name, cond.Status, cond.Reason, cond.Message))
		}
	}

	check("DiscoveryReady", r.GetCondition(ConditionTypeTargetDiscoveryReady))
	check("DatastoreReady", r.GetCondition(ConditionTypeTargetDatastoreReady))
	check("ConfigReady", r.GetCondition(ConditionTypeTargetConfigRecoveryReady))
	check("ConnectionReady", r.GetCondition(ConditionTypeTargetConnectionReady))

	if len(reasons) == 0 {
		return "All conditions are ready"
	}

	return strings.Join(reasons, "; ")
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
			APIVersion: SchemeGroupVersion.Identifier(),
			Kind:       TargetKind,
		},
		ObjectMeta: meta,
		Spec:       spec,
		Status:     status,
	}
}
