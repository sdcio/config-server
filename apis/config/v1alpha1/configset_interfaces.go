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

	"github.com/iptecharch/config-server/pkg/testhelper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
)

const ConfigSetPlural = "configsets"

var _ resource.Object = &ConfigSet{}
var _ resource.ObjectList = &ConfigSetList{}

func (ConfigSet) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    SchemeGroupVersion.Group,
		Version:  SchemeGroupVersion.Version,
		Resource: ConfigSetPlural,
	}
}

// IsStorageVersion returns true -- v1alpha1.Config is used as the internal version.
// IsStorageVersion implements resource.Object.
func (ConfigSet) IsStorageVersion() bool {
	return true
}

// GetObjectMeta implements resource.Object
func (r *ConfigSet) GetObjectMeta() *metav1.ObjectMeta {
	return &r.ObjectMeta
}

// NamespaceScoped returns true to indicate Fortune is a namespaced resource.
// NamespaceScoped implements resource.Object.
func (ConfigSet) NamespaceScoped() bool {
	return true
}

// New implements resource.Object
func (ConfigSet) New() runtime.Object {
	return &ConfigSet{}
}

// NewList implements resource.Object
func (ConfigSet) NewList() runtime.Object {
	return &ConfigSetList{}
}

// GetCondition returns the condition based on the condition kind
func (r *ConfigSet) GetCondition(t ConditionType) Condition {
	return r.Status.GetCondition(t)
}

// SetConditions sets the conditions on the resource. it allows for 0, 1 or more conditions
// to be set at once
func (r *ConfigSet) SetConditions(c ...Condition) {
	r.Status.SetConditions(c...)
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

// GetListMeta returns the ListMeta
func (r *ConfigSetList) GetListMeta() *metav1.ListMeta {
	return &r.ListMeta
}

// BuildConfig returns a reource from a client Object a Spec/Status
func BuildConfigSet(meta metav1.ObjectMeta, spec ConfigSetSpec, status ConfigSetStatus) *ConfigSet {
	return &ConfigSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeBuilder.GroupVersion.Identifier(),
			Kind:       ConfigSetKind,
		},
		ObjectMeta: meta,
		Spec:       spec,
		Status:     status,
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
