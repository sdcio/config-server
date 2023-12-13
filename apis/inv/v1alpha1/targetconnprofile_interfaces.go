/*
Copyright 2023 The Nephio Authors.

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
)

// DefaultTargetConnectionProfile returns a default TargetConnectionProfile
func DefaultTargetConnectionProfile() *TargetConnectionProfile {
	return BuildTargetConnectionProfile(
		metav1.ObjectMeta{
			Name:      "default",
			Namespace: "default",
		},
		TargetConnectionProfileSpec{
			Protocol:   Protocol_GNMI,
			Encoding:   Encoding_Ascii,
			Insecure:   false,
			SkipVerify: true,
		},
	)
}

// BuildTargetConnectionProfile returns a TargetConnectionProfile from a client Object a crName and
// an TargetConnectionProfile Spec/Status
func BuildTargetConnectionProfile(meta metav1.ObjectMeta, spec TargetConnectionProfileSpec) *TargetConnectionProfile {
	return &TargetConnectionProfile{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeBuilder.GroupVersion.Identifier(),
			Kind:       TargetConnectionProfileKind,
		},
		ObjectMeta: meta,
		Spec:       spec,
	}
}
