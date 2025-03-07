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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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

// +k8s:deepcopy-gen=false
var _ ConfigDeviations = &UnManagedConfig{}

func (r *UnManagedConfig) SetDeviations(d []Deviation) {
	r.Status.Deviations = d
}

func (r *UnManagedConfig) DeepObjectCopy() client.Object {
	return r.DeepCopy()
}
