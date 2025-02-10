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
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// GetCondition returns the condition based on the condition kind
func (r *Rollout) GetCondition(t condv1alpha1.ConditionType) condv1alpha1.Condition {
	return r.Status.GetCondition(t)
}

// SetConditions sets the conditions on the resource. it allows for 0, 1 or more conditions
// to be set at once
func (r *Rollout) SetConditions(c ...condv1alpha1.Condition) {
	r.Status.SetConditions(c...)
}

func (r *Rollout) GetNamespacedName() types.NamespacedName {
	return types.NamespacedName{Namespace: r.Namespace, Name: r.Name}
}

func (r *Rollout) GetSkipUnavailableTarget() bool {
	if r.Spec.SkipUnavailableTarget != nil && *r.Spec.SkipUnavailableTarget {
		return true
	}
	return false
}

// BuildRollout returns a Rollout from a client Object a crName and
// an ROllout Spec/Status
func BuildRollout(meta metav1.ObjectMeta, spec RolloutSpec) *Rollout {
	return &Rollout{
		TypeMeta: metav1.TypeMeta{
			APIVersion: localSchemeBuilder.GroupVersion.Identifier(),
			Kind:       RolloutKind,
		},
		ObjectMeta: meta,
		Spec:       spec,
	}
}
