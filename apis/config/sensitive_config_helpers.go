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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

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

func (r *SensitiveConfig) GetNamespacedName() types.NamespacedName {
	return types.NamespacedName{Name: r.Name, Namespace: r.Namespace}
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
