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
	strings "strings"

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

func (r *Target) GetNamespacedName() types.NamespacedName {
	return types.NamespacedName{Namespace: r.Namespace, Name: r.Name}
}

func (r *Target) SetOverallStatus(target *Target) {
	ready := true
	msg := "target ready"
	if ready && !target.GetCondition(ConditionTypeDiscoveryReady).IsTrue() {
		ready = false
		msg = fmt.Sprintf("discovery not ready")// target.GetCondition(ConditionTypeDiscoveryReady).Message)
	}
	if ready && !target.GetCondition(ConditionTypeDatastoreReady).IsTrue() {
		ready = false
		msg = fmt.Sprintf("datastore not ready") // target.GetCondition(ConditionTypeDatastoreReady).Message)
	}
	if ready && !target.GetCondition(ConditionTypeConfigReady).IsTrue() {
		ready = false
		msg = fmt.Sprintf("config not ready")//, target.GetCondition(ConditionTypeConfigReady).Message)
	}
	if ready && !target.GetCondition(ConditionTypeTargetConnectionReady).IsTrue() {
		ready = false
		msg =fmt.Sprintf("datastore connection not ready")// target.GetCondition(ConditionTypeTargetConnectionReady).Message)
	}
	if ready {
		r.Status.SetConditions(condv1alpha1.Ready())
	} else {
		r.Status.SetConditions(condv1alpha1.Failed(msg))
	}
}

func (r *Target) SetStatusResourceVersion() {
	r.Status.ResourceVersion = ptr.To(r.ObjectMeta.ResourceVersion)
}

func (r *Target) SetStatusGeneration() {
	r.Status.Generation = ptr.To(r.ObjectMeta.Generation)
}

func (r *Target) HasResourceversionAndGenerationChanged() bool {
	return r.Status.ResourceVersion != nil && *r.Status.ResourceVersion != r.ObjectMeta.ResourceVersion &&
		r.Status.Generation != nil && *r.Status.Generation != r.ObjectMeta.Generation
}

func (r *Target) IsDatastoreReady() bool {
	return r.GetCondition(ConditionTypeDiscoveryReady).Status == metav1.ConditionTrue &&
		r.GetCondition(ConditionTypeDatastoreReady).Status == metav1.ConditionTrue &&
		r.GetCondition(ConditionTypeTargetConnectionReady).Status == metav1.ConditionTrue
}

func (r *Target) IsReady() bool {
	return r.GetCondition(condv1alpha1.ConditionTypeReady).Status == metav1.ConditionTrue
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

	check("DiscoveryReady", r.GetCondition(ConditionTypeDiscoveryReady))
	check("DatastoreReady", r.GetCondition(ConditionTypeDatastoreReady))
	check("ConfigReady", r.GetCondition(ConditionTypeConfigReady))
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
			APIVersion: localSchemeBuilder.GroupVersion.Identifier(),
			Kind:       TargetKind,
		},
		ObjectMeta: meta,
		Spec:       spec,
		Status:     status,
	}
}
