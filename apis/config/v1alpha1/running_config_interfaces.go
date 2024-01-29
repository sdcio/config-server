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
	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const RunningConfigPlural = "runningconfigs"

// +k8s:deepcopy-gen=false
var _ resource.Object = &RunningConfig{}
var _ resource.ObjectList = &RunningConfigList{}

func (RunningConfig) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    SchemeGroupVersion.Group,
		Version:  SchemeGroupVersion.Version,
		Resource: RunningConfigPlural,
	}
}

// IsStorageVersion returns true -- v1alpha1.RunningConfig is used as the internal version.
// IsStorageVersion implements resource.Object.
func (RunningConfig) IsStorageVersion() bool {
	return true
}

// GetObjectMeta implements resource.Object
func (r *RunningConfig) GetObjectMeta() *metav1.ObjectMeta {
	return &r.ObjectMeta
}

// NamespaceScoped returns true to indicate Fortune is a namespaced resource.
// NamespaceScoped implements resource.Object.
func (RunningConfig) NamespaceScoped() bool {
	return true
}

// New implements resource.Object
func (RunningConfig) New() runtime.Object {
	return &RunningConfig{}
}

// NewList implements resource.Object
func (RunningConfig) NewList() runtime.Object {
	return &RunningConfigList{}
}

// GetListMeta returns the ListMeta
func (r *RunningConfigList) GetListMeta() *metav1.ListMeta {
	return &r.ListMeta
}

// BuildRunningConfig returns a reource from a client Object a Spec/Status
func BuildRunningConfig(meta metav1.ObjectMeta, spec RunningConfigSpec, status RunningConfigStatus) *RunningConfig {
	return &RunningConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeBuilder.GroupVersion.Identifier(),
			Kind:       RunningConfigKind,
		},
		ObjectMeta: meta,
		Spec:       spec,
		Status:     status,
	}
}
