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

// IsReady reports whether the target is fully instantiated: discovered,
// datastore created and configs recovered. Ready aggregates all of that
// (discovery is the first stage of the Ready pipeline), so it is the only
// condition to check.
func (r *Target) IsReady() bool {
	return r.IsConditionReady()
}

// NotReadyReason returns a human readable explanation of why the target is
// not ready, taken from the Ready condition.
func (r *Target) NotReadyReason() string {
	cond := r.GetCondition(condv1alpha1.ConditionTypeReady)
	if cond.Status == metav1.ConditionTrue {
		return "target ready"
	}
	if cond.Reason == "" {
		return "target not ready"
	}
	if cond.Message == "" {
		return cond.Reason
	}
	return fmt.Sprintf("%s: %s", cond.Reason, cond.Message)
}

// GetOwnerReference returns an owner reference to this Target.
// APIVersion/Kind come from the scheme, not TypeMeta: objects read through a
// typed client usually have an empty TypeMeta, which yields an invalid owner
// reference that garbage collection cannot resolve.
func (r *Target) GetOwnerReference() metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: SchemeGroupVersion.String(),
		Kind:       TargetKind,
		Name:       r.Name,
		UID:        r.UID,
		Controller: ptr.To(true),
	}
}

func (r *Target) GetNamespacedName() types.NamespacedName {
	return types.NamespacedName{Namespace: r.Namespace, Name: r.Name}
}

func (r *TargetList) GetItems() []client.Object {
	objs := make([]client.Object, 0, len(r.Items))
	for i := range r.Items {
		objs = append(objs, &r.Items[i])
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
