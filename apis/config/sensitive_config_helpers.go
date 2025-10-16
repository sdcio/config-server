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
	"context"
	"crypto/sha1"
	"encoding/json"
	"os"
	"strings"

	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/apis/condition"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

// GetCondition returns the condition based on the condition kind
func (r *SensitiveConfig) GetCondition(t condition.ConditionType) condition.Condition {
	return r.Status.GetCondition(t)
}

// SetConditions sets the conditions on the resource. it allows for 0, 1 or more conditions
// to be set at once
func (r *SensitiveConfig) SetConditions(c ...condition.Condition) {
	r.Status.SetConditions(c...)
}

func (r *SensitiveConfig) IsConditionReady() bool {
	return r.GetCondition(condition.ConditionTypeReady).Status == metav1.ConditionTrue
}

func (r *SensitiveConfig) IsRevertive() bool {
	if r.Spec.Revertive != nil {
		return *r.Spec.Revertive
	}
	if revertive, found := os.LookupEnv("REVERTIVE"); found {
		if strings.ToLower(revertive) == "false" {
			return false
		}
	}
	return true
}

func (r *SensitiveConfig) IsRecoverable() bool {
	c := r.GetCondition(condition.ConditionTypeReady)
	if c.Reason == string(condition.ConditionReasonUnrecoverable) {
		unrecoverableMessage := &condition.UnrecoverableMessage{}
		if err := json.Unmarshal([]byte(c.Message), unrecoverableMessage); err != nil {
			return true
		}
		if unrecoverableMessage.ResourceVersion != r.GetResourceVersion() {
			return true
		}
		return false
	}
	return true
}

func (r *SensitiveConfig) GetNamespacedName() types.NamespacedName {
	return types.NamespacedName{Name: r.Name, Namespace: r.Namespace}
}

func (r *SensitiveConfig) GetLastKnownGoodSchema() *ConfigStatusLastKnownGoodSchema {
	if r.Status.LastKnownGoodSchema == nil {
		return &ConfigStatusLastKnownGoodSchema{}
	}
	return r.Status.LastKnownGoodSchema
}

func (r *SensitiveConfig) GetTarget() string {
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

func (r *SensitiveConfig) Orphan() bool {
	if r.Spec.Lifecycle != nil {
		return r.Spec.Lifecycle.DeletionPolicy == DeletionOrphan
	}
	return false
}

// BuildSensitiveConfig returns a reource from a client Object a Spec/Status
func BuildSensitiveConfig(meta metav1.ObjectMeta, spec SensitiveConfigSpec) *SensitiveConfig {
	return &SensitiveConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.Identifier(),
			Kind:       SensitiveConfigKind,
		},
		ObjectMeta: meta,
		Spec:       spec,
	}
}

func BuildEmptySensitiveConfig() *SensitiveConfig {
	return &SensitiveConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.Identifier(),
			Kind:       SensitiveConfigKind,
		},
	}
}

func (r *SensitiveConfig) HashDeviationGenerationChanged(deviation Deviation) bool {
	if r.Status.DeviationGeneration == nil {
		// if there was no old deviation, but now we have a deviation wwe return true
		return len(deviation.Spec.Deviations) != 0
	} else {
		return *r.Status.DeviationGeneration == deviation.GetGeneration()
	}
}

type RecoverySensitiveConfig struct {
	SensitiveConfig SensitiveConfig
	Deviation       Deviation
}

func (r *SensitiveConfigSpec) GetShaSum(ctx context.Context) [20]byte {
	log := log.FromContext(ctx)
	appliedSpec, err := json.Marshal(r)
	if err != nil {
		log.Error("cannot marshal appliedConfig", "error", err)
		return [20]byte{}
	}
	return sha1.Sum(appliedSpec)
}

func (r *SensitiveConfig) GetOwnerReference() metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: SchemeGroupVersion.Identifier(),
		Kind:       SensitiveConfigKind,
		Name:       r.Name,
		UID:        r.UID,
		Controller: ptr.To(true),
	}
}
