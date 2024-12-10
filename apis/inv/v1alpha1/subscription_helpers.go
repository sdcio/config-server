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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

// GetCondition returns the condition based on the condition kind
func (r *Subscription) GetCondition(t condv1alpha1.ConditionType) condv1alpha1.Condition {
	return r.Status.GetCondition(t)
}

// SetConditions sets the conditions on the resource. it allows for 0, 1 or more conditions
// to be set at once
func (r *Subscription) SetConditions(c ...condv1alpha1.Condition) {
	r.Status.SetConditions(c...)
}

// GetNamespacedName
func (r *Subscription) GetNamespacedName() types.NamespacedName {
	return types.NamespacedName{Namespace: r.Namespace, Name: r.Name}
}

func (r *Subscription) SetTargets(targets []string) {
	r.Status.Targets = targets
}

func (r *SubscriptionParameters) GetIntervalSeconds() int {
	if r.Mode == SyncMode_OnChange {
		return 0
	}
	if r.Interval == nil {
		// default os 15 sec
		return 15
	}
	return int(r.Interval.Duration.Seconds())
}

func (r *Subscription) GetExistingTargets() sets.String {
	existingTargetSet := sets.NewString()
	for _, targetname := range r.Status.Targets {
		existingTargetSet.Insert(targetname)
	}
	return existingTargetSet
}

func (r *SubscriptionParameters) IsEnabled() bool {
	if r.AdminState == nil {
		return false
	}
	return *r.AdminState == AdminState_ENABLED
}

func (r *Subscription) GetEncoding() Encoding {
	if r.Spec.Encoding == nil {
		return Encoding_ASCII
	}
	return *r.Spec.Encoding
}
