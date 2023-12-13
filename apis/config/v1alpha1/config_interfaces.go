/*
Copyright 2023 The xxx Authors.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/apiserver-runtime/pkg/builder/resource"
)

const ConfigPlural = "configs"

var _ resource.Object = &Config{}
var _ resource.ObjectList = &ConfigList{}

func (Config) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    SchemeGroupVersion.Group,
		Version:  SchemeGroupVersion.Version,
		Resource: ConfigPlural,
	}
}

// IsStorageVersion returns true -- v1alpha1.Config is used as the internal version.
// IsStorageVersion implements resource.Object.
func (Config) IsStorageVersion() bool {
	return true
}

// GetObjectMeta implements resource.Object
func (r *Config) GetObjectMeta() *metav1.ObjectMeta {
	return &r.ObjectMeta
}

// NamespaceScoped returns true to indicate Fortune is a namespaced resource.
// NamespaceScoped implements resource.Object.
func (Config) NamespaceScoped() bool {
	return true
}

// New implements resource.Object
func (Config) New() runtime.Object {
	return &Config{}
}

// NewList implements resource.Object
func (Config) NewList() runtime.Object {
	return &ConfigList{}
}

// GetListMeta returns the ListMeta
func (r *ConfigList) GetListMeta() *metav1.ListMeta {
	return &r.ListMeta
}