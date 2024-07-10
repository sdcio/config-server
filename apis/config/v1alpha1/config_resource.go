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
	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	"github.com/sdcio/config-server/apis/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// +k8s:deepcopy-gen=false
var _ resource.Object = &Config{}
var _ resource.ObjectList = &ConfigList{}
var _ resource.MultiVersionObject = &Config{}

func (Config) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    SchemeGroupVersion.Group,
		Version:  SchemeGroupVersion.Version,
		Resource: config.ConfigPlural,
	}
}

// IsStorageVersion returns true -- Config is used as the internal version.
// IsStorageVersion implements resource.Object
func (Config) IsStorageVersion() bool {
	return false
}

// NamespaceScoped returns true to indicate Fortune is a namespaced resource.
// NamespaceScoped implements resource.Object
func (Config) NamespaceScoped() bool {
	return true
}

// GetObjectMeta implements resource.Object
// GetObjectMeta implements resource.Object
func (r *Config) GetObjectMeta() *metav1.ObjectMeta {
	return &r.ObjectMeta
}

// New return an empty resource
// New implements resource.Object
func (Config) New() runtime.Object {
	return &Config{}
}

// NewList return an empty resourceList
// NewList implements resource.Object
func (Config) NewList() runtime.Object {
	return &ConfigList{}
}

// GetListMeta returns the ListMeta
// GetListMeta implements resource.ObjectList
func (r *ConfigList) GetListMeta() *metav1.ListMeta {
	return &r.ListMeta
}

// NewStorageVersionObject returns a new empty instance of storage version.
// NewStorageVersionObject implements resource.MultiVersionObject
func (r *Config) NewStorageVersionObject() runtime.Object {
	return r.New()
}

// ConvertToStorageVersion receives an new instance of storage version object as the conversion target
// and overwrites it to the equal form of the current resource version.
func (r *Config) ConvertToStorageVersion(storageObj runtime.Object) error {
	return Convert_v1alpha1_Config_To_config_Config(r, storageObj.(*config.Config), nil)
}

// ConvertFromStorageVersion receives an instance of storage version as the conversion source and
// in-place mutates the current object to the equal form of the storage version object.
func (r *Config) ConvertFromStorageVersion(storageObj runtime.Object) error {
	return Convert_config_Config_To_v1alpha1_Config(storageObj.(*config.Config), r, nil)
}