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
var _ resource.Object = &RunningConfig{}
var _ resource.ObjectList = &RunningConfigList{}

func (RunningConfig) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    SchemeGroupVersion.Group,
		Version:  SchemeGroupVersion.Version,
		Resource: config.RunningConfigPlural,
	}
}

// IsStorageVersion returns true -- Config is used as the internal version.
// IsStorageVersion implements resource.Object
func (RunningConfig) IsStorageVersion() bool {
	return false
}

// NamespaceScoped returns true to indicate Fortune is a namespaced resource.
// NamespaceScoped implements resource.Object
func (RunningConfig) NamespaceScoped() bool {
	return true
}

// GetObjectMeta implements resource.Object
// GetObjectMeta implements resource.Object
func (r *RunningConfig) GetObjectMeta() *metav1.ObjectMeta {
	return &r.ObjectMeta
}

// New return an empty resource
// New implements resource.Object
func (RunningConfig) New() runtime.Object {
	return &Config{}
}

// NewList return an empty resourceList
// NewList implements resource.Object
func (RunningConfig) NewList() runtime.Object {
	return &ConfigList{}
}

// GetListMeta returns the ListMeta
func (r *RunningConfigList) GetListMeta() *metav1.ListMeta {
	return &r.ListMeta
}

// NewStorageVersionObject returns a new empty instance of storage version.
// NewStorageVersionObject implements resource.MultiVersionObject
func (r *RunningConfig) NewStorageVersionObject() runtime.Object {
	return r.New()
}

// ConvertToStorageVersion receives an new instance of storage version object as the conversion target
// and overwrites it to the equal form of the current resource version.
// ConvertToStorageVersion implements resource.MultiVersionObject
func (r *RunningConfig) ConvertToStorageVersion(storageObj runtime.Object) error {
	return Convert_v1alpha1_RunningConfig_To_config_RunningConfig(r, storageObj.(*config.RunningConfig), nil)
}

// ConvertFromStorageVersion receives an instance of storage version as the conversion source and
// in-place mutates the current object to the equal form of the storage version object.
// ConvertFromStorageVersion implements resource.MultiVersionObject
func (r *RunningConfig) ConvertFromStorageVersion(storageObj runtime.Object) error {
	return Convert_config_RunningConfig_To_v1alpha1_RunningConfig(storageObj.(*config.RunningConfig), r, nil)
}