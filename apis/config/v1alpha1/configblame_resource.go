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
var _ resource.Object = &ConfigBlame{}
var _ resource.ObjectList = &ConfigBlameList{}
var _ resource.MultiVersionObject = &ConfigBlame{}

func (ConfigBlame) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    SchemeGroupVersion.Group,
		Version:  SchemeGroupVersion.Version,
		Resource: config.ConfigBlamePlural,
	}
}

// IsStorageVersion returns true -- Config is used as the internal version.
// IsStorageVersion implements resource.Object
func (ConfigBlame) IsStorageVersion() bool {
	return false
}

// NamespaceScoped returns true to indicate Fortune is a namespaced resource.
// NamespaceScoped implements resource.Object
func (ConfigBlame) NamespaceScoped() bool {
	return true
}

// GetObjectMeta implements resource.Object
// GetObjectMeta implements resource.Object
func (r *ConfigBlame) GetObjectMeta() *metav1.ObjectMeta {
	return &r.ObjectMeta
}

// New return an empty resource
// New implements resource.Object
func (ConfigBlame) New() runtime.Object {
	return &ConfigBlame{}
}

// NewList return an empty resourceList
// NewList implements resource.Object
func (ConfigBlame) NewList() runtime.Object {
	return &ConfigBlameList{}
}

// GetListMeta returns the ListMeta
func (r *ConfigBlameList) GetListMeta() *metav1.ListMeta {
	return &r.ListMeta
}

// RegisterConversions registers the conversions.
// RegisterConversions implements resource.MultiVersionObject
func (ConfigBlame) RegisterConversions() func(s *runtime.Scheme) error {
	return RegisterConversions
}
