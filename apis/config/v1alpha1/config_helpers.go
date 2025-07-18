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
	"path/filepath"
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
func (r *Config) GetCondition(t condv1alpha1.ConditionType) condv1alpha1.Condition {
	return r.Status.GetCondition(t)
}

// SetConditions sets the conditions on the resource. it allows for 0, 1 or more conditions
// to be set at once
func (r *Config) SetConditions(c ...condv1alpha1.Condition) {
	r.Status.SetConditions(c...)
}

func (r *Config) IsConditionReady() bool {
	return r.GetCondition(condv1alpha1.ConditionTypeReady).Status == metav1.ConditionTrue
}

func (r *Config) GetOwnerReference() metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: r.TypeMeta.APIVersion,
		Kind:       r.TypeMeta.Kind,
		Name:       r.Name,
		UID:        r.UID,
		Controller: ptr.To(true),
	}
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

func (r *Config) IsRecoverable() bool {
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

func (r *Config) HashDeviationGenerationChanged(deviation Deviation) bool {
	if r.Status.DeviationGeneration == nil {
		// if there was no old deviation, but now we have a deviation wwe return true
		return len(deviation.Spec.Deviations) != 0
	} else {
		return *r.Status.DeviationGeneration == deviation.GetGeneration()
	}
}

func (r *Config) GetNamespacedName() types.NamespacedName {
	return types.NamespacedName{Namespace: r.Namespace, Name: r.Name}
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

func (r *Config) GetTargetNamespaceName() (*types.NamespacedName, error) {
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
		Name: targetName,
		Namespace: targetNamespace,
	}, nil
}

/*
// IsTransacting return true if a create/update or delete is ongoing on the config object
func (r *Config) IsTransacting() bool {
	condition := r.GetCondition(condv1alpha1.ConditionTypeReady)
	return condition.Reason == string(condv1alpha1.ConditionReasonCreating) ||
		condition.Reason == string(condv1alpha1.ConditionReasonUpdating) ||
		condition.Reason == string(condv1alpha1.ConditionReasonDeleting)
}
*/

func (r *Config) Validate() error {
	var errm error
	if _, ok := r.GetLabels()[config.TargetNameKey]; !ok {
		errm = errors.Join(errm, fmt.Errorf("a config cr always need a %s", config.TargetNameKey))
	}
	if _, ok := r.GetLabels()[config.TargetNamespaceKey]; !ok {
		errm = errors.Join(errm, fmt.Errorf("a config cr always need a %s", config.TargetNamespaceKey))
	}
	return errm
}

func (r *ConfigStatusLastKnownGoodSchema) FileString() string {
	return filepath.Join(r.Type, r.Vendor, r.Version)
}

// BuildConfig returns a reource from a client Object a Spec/Status
func BuildConfig(meta metav1.ObjectMeta, spec ConfigSpec, status ConfigStatus) *Config {
	return &Config{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.Identifier(),
			Kind:       ConfigKind,
		},
		ObjectMeta: meta,
		Spec:       spec,
		Status:     status,
	}
}

// GetConfigFromFile is a helper for tests to use the
// examples and validate them in unit tests
func GetConfigFromFile(path string) (*Config, error) {
	addToScheme := AddToScheme
	obj := &Config{}
	gvk := SchemeGroupVersion.WithKind(reflect.TypeOf(obj).Name())
	// build object from file
	if err := testhelper.GetKRMResource(path, obj, gvk, addToScheme); err != nil {
		return nil, err
	}
	return obj, nil
}

// GetShaSum calculates the shasum of the confgiSpec
func (r *ConfigSpec) GetShaSum(ctx context.Context) [20]byte {
	log := log.FromContext(ctx)
	appliedSpec, err := json.Marshal(r)
	if err != nil {
		log.Error("cannot marshal appliedConfig", "error", err)
		return [20]byte{}
	}
	return sha1.Sum(appliedSpec)
}

// ConvertConfigFieldSelector is the schema conversion function for normalizing the FieldSelector for Config
func ConvertConfigFieldSelector(label, value string) (internalLabel, internalValue string, err error) {
	switch label {
	case "metadata.name":
		return label, value, nil
	case "metadata.namespace":
		return label, value, nil
	default:
		return "", "", fmt.Errorf("%q is not a known field selector", label)
	}
}

func (r *Config) CalculateHash() ([sha1.Size]byte, error) {
	// Convert the struct to JSON
	jsonData, err := json.Marshal(r)
	if err != nil {
		return [sha1.Size]byte{}, err
	}

	// Calculate SHA-1 hash
	return sha1.Sum(jsonData), nil
}

/*
	func ConvertSdcpbDeviations2ConfigDeviations(devs []*sdcpb.WatchDeviationResponse) []Deviation {
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

	func (r ConfigStatus) HasNotAppliedDeviation() bool {
		for _, dev := range r.Deviations {
			if dev.Reason == "NOT_APPLIED" {
				return true
			}
		}
		return false
	}

// +k8s:deepcopy-gen=false
var _ ConfigDeviations = &Config{}

	func (r *Config) SetDeviations(d []Deviation) {
		r.Status.Deviations = d
	}
*/
func (r *Config) DeepObjectCopy() client.Object {
	return r.DeepCopy()
}
