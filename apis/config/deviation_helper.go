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
	"strings"

	"github.com/sdcio/config-server/apis/condition"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// GetCondition returns the condition based on the condition kind
func (r *Deviation) GetCondition(t condition.ConditionType) condition.Condition {
	return r.Status.GetCondition(t)
}

// SetConditions sets the conditions on the resource. it allows for 0, 1 or more conditions
// to be set at once
func (r *Deviation) SetConditions(c ...condition.Condition) {
	r.Status.SetConditions(c...)
}

func (r *Deviation) IsConditionReady() bool {
	return r.GetCondition(condition.ConditionTypeReady).Status == metav1.ConditionTrue
}

func (r *Deviation) GetNamespacedName() types.NamespacedName {
	return types.NamespacedName{Name: r.Name, Namespace: r.Namespace}
}

func (r *Deviation) GetDeviationType() string {
	if r.Spec.DeviationType != nil {
		return r.Spec.DeviationType.String()
	}
	return DeviationType_TARGET.String()
}

func (r *Deviation) GetTarget() string {
	if len(r.GetLabels()) == 0 {
		return ""
	}
	var sb strings.Builder
	targetNamespace, ok := r.GetLabels()[TargetNamespaceKey]
	if ok {
		sb.WriteString(targetNamespace)
		sb.WriteString("/")
	}
	targetName, ok := r.GetLabels()[TargetNameKey]
	if ok {
		sb.WriteString(targetName)
	}
	return sb.String()
}

func (r Deviation) HasNotAppliedDeviation() bool {
	for _, dev := range r.Spec.Deviations {
		if dev.Reason == "NOT_APPLIED" {
			return true
		}
	}
	return false
}

func BuildDeviation(meta metav1.ObjectMeta, spec *DeviationSpec, status *DeviationStatus) *Deviation {
	if spec == nil {
		spec = &DeviationSpec{}
	}
	if status == nil {
		status = &DeviationStatus{}
	}

	return &Deviation{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.Identifier(),
			Kind:       DeviationKind,
		},
		ObjectMeta: meta,
		Spec:       *spec,
		Status:     *status,
	}
}

func BuildEmptyDeviation() *Deviation {
	return &Deviation{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.Identifier(),
			Kind:       DeviationKind,
		},
	}
}
