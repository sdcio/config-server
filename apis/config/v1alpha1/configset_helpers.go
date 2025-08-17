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
	"reflect"
	"strings"

	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	"github.com/sdcio/config-server/pkg/testhelper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// GetCondition returns the condition based on the condition kind
func (r *ConfigSet) GetCondition(t condv1alpha1.ConditionType) condv1alpha1.Condition {
	return r.Status.GetCondition(t)
}

// SetConditions sets the conditions on the resource. it allows for 0, 1 or more conditions
// to be set at once
func (r *ConfigSet) SetConditions(c ...condv1alpha1.Condition) {
	r.Status.SetConditions(c...)
}

func (r *ConfigSet) GetNamespacedName() types.NamespacedName {
	return types.NamespacedName{Namespace: r.Namespace, Name: r.Name}
}

func (r *ConfigSet) GetTarget() string {
	if len(r.Labels) == 0 {
		return ""
	}
	var sb strings.Builder
	targetNamespace, ok := r.Labels["targetNamespace"]
	if ok {
		sb.WriteString(targetNamespace)
		sb.WriteString("/")
	}
	targetName, ok := r.Labels["targetName"]
	if ok {
		sb.WriteString(targetName)
	}
	return sb.String()
}

// BuildConfig returns a reource from a client Object a Spec/Status
func BuildConfigSet(meta metav1.ObjectMeta, spec ConfigSetSpec, status ConfigSetStatus) *ConfigSet {
	return &ConfigSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.Identifier(),
			Kind:       ConfigSetKind,
		},
		ObjectMeta: meta,
		Spec:       spec,
		Status:     status,
	}
}

// BuildEmptyConfigSet returns an empty configset
func BuildEmptyConfigSet() *ConfigSet {
	return &ConfigSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.Identifier(),
			Kind:       ConfigSetKind,
		},
	}
}

// GetConfigSetFromFile is a helper for tests to use the
// examples and validate them in unit tests
func GetConfigSetFromFile(path string) (*ConfigSet, error) {
	addToScheme := AddToScheme
	obj := &ConfigSet{}
	gvk := SchemeGroupVersion.WithKind(reflect.TypeOf(obj).Name())
	// build object from file
	if err := testhelper.GetKRMResource(path, obj, gvk, addToScheme); err != nil {
		return nil, err
	}
	return obj, nil
}
