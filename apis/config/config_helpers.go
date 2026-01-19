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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/apis/condition"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

// GetCondition returns the condition based on the condition kind
func (r *Config) GetCondition(t condition.ConditionType) condition.Condition {
	return r.Status.GetCondition(t)
}

// SetConditions sets the conditions on the resource. it allows for 0, 1 or more conditions
// to be set at once
func (r *Config) SetConditions(c ...condition.Condition) {
	r.Status.SetConditions(c...)
}

func (r *Config) IsConditionReady() bool {
	return r.GetCondition(condition.ConditionTypeReady).Status == metav1.ConditionTrue
}

func (r *Config) IsRevertive() bool {
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

func (r *Config) IsRecoverable(ctx context.Context) bool {
	logger := log.FromContext(ctx)
	c := r.GetCondition(condition.ConditionTypeReady)
	if c.Reason == string(condition.ConditionReasonUnrecoverable) {
		unrecoverableMessage := &condition.UnrecoverableMessage{}
		if err := json.Unmarshal([]byte(c.Message), unrecoverableMessage); err != nil {
			logger.Error("is recoverable json unmarchal failed", "error", err)
			return true
		}
		if unrecoverableMessage.ResourceVersion != r.GetResourceVersion() {
			logger.Info("is recoverable resource version changed", "old/new", 
				fmt.Sprintf("%s/%s", unrecoverableMessage.ResourceVersion, r.GetResourceVersion()))
			return true
		}
		return false
	}
	return true
}

func (r *Config) GetNamespacedName() types.NamespacedName {
	return types.NamespacedName{Name: r.Name, Namespace: r.Namespace}
}

func (r *Config) GetLastKnownGoodSchema() *ConfigStatusLastKnownGoodSchema {
	if r.Status.LastKnownGoodSchema == nil {
		return &ConfigStatusLastKnownGoodSchema{}
	}
	return r.Status.LastKnownGoodSchema
}

func (r *Config) GetTarget() string {
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

func (r *Config) Orphan() bool {
	if r.Spec.Lifecycle != nil {
		return r.Spec.Lifecycle.DeletionPolicy == DeletionOrphan
	}
	return false
}

func (r *ConfigStatusLastKnownGoodSchema) FileString() string {
	return filepath.Join(r.Type, r.Vendor, r.Version)
}

func GetTargetKey(labels map[string]string) (types.NamespacedName, error) {
	var targetName, targetNamespace string
	if labels != nil {
		targetName = labels[TargetNameKey]
		targetNamespace = labels[TargetNamespaceKey]
	}
	if targetName == "" || targetNamespace == "" {
		return types.NamespacedName{}, fmt.Errorf(" target namespace and name is required got %s.%s", targetNamespace, targetName)
	}
	return types.NamespacedName{
		Namespace: targetNamespace,
		Name:      targetName,
	}, nil
}

// BuildConfig returns a reource from a client Object a Spec/Status
func BuildConfig(meta metav1.ObjectMeta, spec ConfigSpec) *Config {
	return &Config{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.Identifier(),
			Kind:       ConfigKind,
		},
		ObjectMeta: meta,
		Spec:       spec,
	}
}

func BuildEmptyConfig() *Config {
	return &Config{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.Identifier(),
			Kind:       ConfigKind,
		},
	}
}

type RecoveryConfig struct {
	Config    Config
	Deviation Deviation
}

func (r *ConfigSpec) GetShaSum(ctx context.Context) [20]byte {
	log := log.FromContext(ctx)
	appliedSpec, err := json.Marshal(r)
	if err != nil {
		log.Error("cannot marshal appliedConfig", "error", err)
		return [20]byte{}
	}
	return sha1.Sum(appliedSpec)
}

func (r *Config) GetOwnerReference() metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: "config.sdcio.dev/v1alpha1",
		Kind:       "Config",
		Name:       r.Name,
		UID:        r.UID,
		Controller: ptr.To(true),
	}
}