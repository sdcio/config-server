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
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/apis/config"
	"github.com/sdcio/config-server/pkg/testhelper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *SensitiveConfig) GetOwnerReference() metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: r.APIVersion,
		Kind:       r.Kind,
		Name:       r.Name,
		UID:        r.UID,
		Controller: ptr.To(true),
	}
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

func (r *SensitiveConfig) GetNamespacedName() types.NamespacedName {
	return types.NamespacedName{Namespace: r.Namespace, Name: r.Name}
}

func (r *SensitiveConfig) GetTarget() string {
	if len(r.GetLabels()) == 0 {
		return ""
	}
	var sb strings.Builder
	targetNamespace, ok := r.GetLabels()[config.TargetNamespaceKey]
	if ok {
		sb.WriteString(targetNamespace)
		sb.WriteString("/")
	}
	targetName, ok := r.GetLabels()[config.TargetNameKey]
	if ok {
		sb.WriteString(targetName)
	}
	return sb.String()
}

func (r *SensitiveConfig) GetTargetNamespaceName() (*types.NamespacedName, error) {
	if len(r.GetLabels()) == 0 {
		return nil, fmt.Errorf("no target information found in labels")
	}
	targetNamespace, ok := r.GetLabels()[config.TargetNamespaceKey]
	if !ok {
		return nil, fmt.Errorf("no target namespece information found in labels")
	}
	targetName, ok := r.GetLabels()[config.TargetNameKey]
	if !ok {
		return nil, fmt.Errorf("no target namespece information found in labels")
	}
	return &types.NamespacedName{
		Name:      targetName,
		Namespace: targetNamespace,
	}, nil
}

func (r *SensitiveConfig) Validate() error {
	var errm error
	if _, ok := r.GetLabels()[config.TargetNameKey]; !ok {
		errm = errors.Join(errm, fmt.Errorf("a SensitiveConfig cr always need a %s", config.TargetNameKey))
	}
	if _, ok := r.GetLabels()[config.TargetNamespaceKey]; !ok {
		errm = errors.Join(errm, fmt.Errorf("a SensitiveConfig cr always need a %s", config.TargetNamespaceKey))
	}
	return errm
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

// BuildEmptySensitiveConfig returns an empty SensitiveConfig
func BuildEmptySensitiveConfig() *SensitiveConfig {
	return &SensitiveConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.Identifier(),
			Kind:       SensitiveConfigKind,
		},
	}
}

// GetSensitiveConfigFromFile is a helper for tests to use the
// examples and validate them in unit tests
func GetSensitiveConfigFromFile(path string) (*SensitiveConfig, error) {
	addToScheme := AddToScheme
	obj := &SensitiveConfig{}
	gvk := SchemeGroupVersion.WithKind(reflect.TypeOf(obj).Name())
	// build object from file
	if err := testhelper.GetKRMResource(path, obj, gvk, addToScheme); err != nil {
		return nil, err
	}
	return obj, nil
}

// GetShaSum calculates the shasum of the confgiSpec
func (r *SensitiveConfigSpec) GetShaSum(ctx context.Context) [20]byte {
	log := log.FromContext(ctx)
	appliedSpec, err := json.Marshal(r)
	if err != nil {
		log.Error("cannot marshal appliedSensitiveConfig", "error", err)
		return [20]byte{}
	}
	return sha1.Sum(appliedSpec)
}

// ConvertSensitiveConfigFieldSelector is the schema conversion function for normalizing the FieldSelector for SensitiveConfig
func ConvertSensitiveConfigFieldSelector(label, value string) (internalLabel, internalValue string, err error) {
	switch label {
	case "metadata.name":
		return label, value, nil
	case "metadata.namespace":
		return label, value, nil
	default:
		return "", "", fmt.Errorf("%q is not a known field selector", label)
	}
}

func (r *SensitiveConfig) CalculateHash() ([sha1.Size]byte, error) {
	// Convert the struct to JSON
	jsonData, err := json.Marshal(r)
	if err != nil {
		return [sha1.Size]byte{}, err
	}

	// Calculate SHA-1 hash
	return sha1.Sum(jsonData), nil
}

func (r *SensitiveConfig) DeepObjectCopy() client.Object {
	return r.DeepCopy()
}
