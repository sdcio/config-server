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

	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const UnManagedConfigPlural = "unmanagedconfigs"
const UnManagedConfigSingular = "unmanagedconfig"

// +k8s:deepcopy-gen=false
var _ resource.Object = &UnManagedConfig{}
var _ resource.ObjectList = &UnManagedConfigList{}

func (r *UnManagedConfig) GetSingularName() string {
	return UnManagedConfigSingular
}

func (UnManagedConfig) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    SchemeGroupVersion.Group,
		Version:  SchemeGroupVersion.Version,
		Resource: UnManagedConfigPlural,
	}
}

// IsStorageVersion returns true -- v1alpha1.UnManagedConfig is used as the internal version.
// IsStorageVersion implements resource.Object.
func (UnManagedConfig) IsStorageVersion() bool {
	return true
}

// GetObjectMeta implements resource.Object
func (r *UnManagedConfig) GetObjectMeta() *metav1.ObjectMeta {
	return &r.ObjectMeta
}

// NamespaceScoped returns true to indicate Fortune is a namespaced resource.
// NamespaceScoped implements resource.Object.
func (UnManagedConfig) NamespaceScoped() bool {
	return true
}

// New implements resource.Object
func (UnManagedConfig) New() runtime.Object {
	return &UnManagedConfig{}
}

// NewList implements resource.Object
func (UnManagedConfig) NewList() runtime.Object {
	return &UnManagedConfigList{}
}

// GetListMeta returns the ListMeta
func (r *UnManagedConfigList) GetListMeta() *metav1.ListMeta {
	return &r.ListMeta
}

// BuildUnManagedConfig returns a reource from a client Object a Spec/Status
func BuildUnManagedConfig(meta metav1.ObjectMeta, spec UnManagedConfigSpec, status UnManagedConfigStatus) *UnManagedConfig {
	return &UnManagedConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.Identifier(),
			Kind:       UnManagedConfigKind,
		},
		ObjectMeta: meta,
		Spec:       spec,
		Status:     status,
	}
}

// ConvertUnManagedConfigFieldSelector is the schema conversion function for normalizing the FieldSelector for UnManagedConfig
func ConvertUnManagedConfigFieldSelector(label, value string) (internalLabel, internalValue string, err error) {
	switch label {
	case "metadata.name":
		return label, value, nil
	case "metadata.namespace":
		return label, value, nil
	default:
		return "", "", fmt.Errorf("%q is not a known field selector", label)
	}
}
