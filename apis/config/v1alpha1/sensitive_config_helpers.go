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
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	"github.com/sdcio/config-server/apis/config"
	"github.com/sdcio/config-server/pkg/testhelper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetCondition returns the condition based on the condition kind
func (r *SensitiveConfig) GetCondition(t condv1alpha1.ConditionType) condv1alpha1.Condition {
	return r.Status.GetCondition(t)
}

// SetConditions sets the conditions on the resource. it allows for 0, 1 or more conditions
// to be set at once
func (r *SensitiveConfig) SetConditions(c ...condv1alpha1.Condition) {
	r.Status.SetConditions(c...)
}

func (r *SensitiveConfig) IsConditionReady() bool {
	return r.GetCondition(condv1alpha1.ConditionTypeReady).Status == metav1.ConditionTrue
}

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

func (r *SensitiveConfig) IsRecoverable() bool {
	c := r.GetCondition(condv1alpha1.ConditionTypeReady)
	if c.Reason == string(condv1alpha1.ConditionReasonUnrecoverable) {
		unrecoverableMessage := &condv1alpha1.UnrecoverableMessage{}
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

func (r *SensitiveConfig) HashDeviationGenerationChanged(deviation Deviation) bool {
	if r.Status.DeviationGeneration == nil {
		// if there was no old deviation, but now we have a deviation wwe return true
		return len(deviation.Spec.Deviations) != 0
	} else {
		return *r.Status.DeviationGeneration == deviation.GetGeneration()
	}
}

func (r *SensitiveConfig) GetNamespacedName() types.NamespacedName {
	return types.NamespacedName{Namespace: r.Namespace, Name: r.Name}
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

/*
// IsTransacting return true if a create/update or delete is ongoing on the SensitiveConfig object
func (r *SensitiveConfig) IsTransacting() bool {
	condition := r.GetCondition(condv1alpha1.ConditionTypeReady)
	return condition.Reason == string(condv1alpha1.ConditionReasonCreating) ||
		condition.Reason == string(condv1alpha1.ConditionReasonUpdating) ||
		condition.Reason == string(condv1alpha1.ConditionReasonDeleting)
}
*/

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
func BuildSensitiveConfig(meta metav1.ObjectMeta, spec SensitiveConfigSpec, status SensitiveConfigStatus) *SensitiveConfig {
	return &SensitiveConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.Identifier(),
			Kind:       SensitiveConfigKind,
		},
		ObjectMeta: meta,
		Spec:       spec,
		Status:     status,
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

/*
	func ConvertSdcpbDeviations2SensitiveConfigDeviations(devs []*sdcpb.WatchDeviationResponse) []Deviation {
		deviations := make([]Deviation, 0, len(devs))
		for _, dev := range devs {
			deviations = append(deviations, Deviation{
				Path:         utils.ToXPath(dev.GetPath(), false),
				DesiredValue: dev.GetExpectedValue().String(),
				CurrentValue: dev.GetCurrentValue().String(),
				Reason:       dev.GetReason().String(),
			})
		}
		return deviations
	}

	func (r SensitiveConfigStatus) HasNotAppliedDeviation() bool {
		for _, dev := range r.Deviations {
			if dev.Reason == "NOT_APPLIED" {
				return true
			}
		}
		return false
	}

// +k8s:deepcopy-gen=false
var _ SensitiveConfigDeviations = &SensitiveConfig{}

	func (r *SensitiveConfig) SetDeviations(d []Deviation) {
		r.Status.Deviations = d
	}
*/
func (r *SensitiveConfig) DeepObjectCopy() client.Object {
	return r.DeepCopy()
}
