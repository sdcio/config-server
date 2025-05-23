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

	"github.com/sdcio/config-server/pkg/testhelper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func (r *TargetConnectionProfile) IsInsecure() bool {
	return r.Spec.Insecure != nil && *r.Spec.Insecure
}

func (r *TargetConnectionProfile) SkipVerify() bool {
	return r.Spec.SkipVerify != nil && *r.Spec.SkipVerify
}

func (r *TargetConnectionProfile) Encoding() Encoding {
	if r.Spec.Encoding == nil {
		return Encoding_JSON_IETF
	}
	return *r.Spec.Encoding
}

func (r *TargetConnectionProfile) IncludeNS() bool {
	return r.Spec.IncludeNS != nil && *r.Spec.IncludeNS
}

func (r *TargetConnectionProfile) OperationWithNS() bool {
	return r.Spec.OperationWithNS != nil && *r.Spec.OperationWithNS
}

func (r *TargetConnectionProfile) UseOperationRemove() bool {
	return r.Spec.UseOperationRemove != nil && *r.Spec.UseOperationRemove
}

func (r *TargetConnectionProfile) CommitCandidate() CommitCandidate {
	if r.Spec.CommitCandidate == nil {
		return CommitCandidate_Candidate
	}
	return *r.Spec.CommitCandidate
}

// DefaultTargetConnectionProfile returns a default TargetConnectionProfile
func DefaultTargetConnectionProfile() *TargetConnectionProfile {
	return BuildTargetConnectionProfile(
		metav1.ObjectMeta{
			Name:      "default",
			Namespace: "default",
		},
		TargetConnectionProfileSpec{
			Protocol:   Protocol_GNMI,
			Encoding:   ptr.To(Encoding_JSON_IETF),
			Insecure:   ptr.To(false),
			SkipVerify: ptr.To(true),
		},
	)
}

// BuildTargetConnectionProfile returns a TargetConnectionProfile from a client Object a crName and
// an TargetConnectionProfile Spec/Status
func BuildTargetConnectionProfile(meta metav1.ObjectMeta, spec TargetConnectionProfileSpec) *TargetConnectionProfile {
	return &TargetConnectionProfile{
		TypeMeta: metav1.TypeMeta{
			APIVersion: localSchemeBuilder.GroupVersion.Identifier(),
			Kind:       TargetConnectionProfileKind,
		},
		ObjectMeta: meta,
		Spec:       spec,
	}
}

// GetTargetConnectionProfileFromFile is a helper for tests to use the
// examples and validate them in unit tests
func GetTargetConnectionProfileFromFile(path string) (*DiscoveryRule, error) {
	addToScheme := AddToScheme
	obj := &DiscoveryRule{}
	gvk := SchemeGroupVersion.WithKind(reflect.TypeOf(obj).Name())
	// build object from file
	if err := testhelper.GetKRMResource(path, obj, gvk, addToScheme); err != nil {
		return nil, err
	}
	return obj, nil
}
