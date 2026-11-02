/*
Copyright 2026 Nokia.

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
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/pkg/testhelper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *TargetSnapshot) GetOwnerReference() metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: r.APIVersion,
		Kind:       r.Kind,
		Name:       r.Name,
		UID:        r.UID,
		Controller: ptr.To(true),
	}
}

func (r *TargetSnapshot) GetNamespacedName() types.NamespacedName {
	return types.NamespacedName{Namespace: r.Namespace, Name: r.Name}
}

func (r *TargetSnapshot) Validate() error {
	return nil
}

// BuildTargetSnapshot returns a reource from a client Object a Spec/Status
func BuildTargetSnapshot(meta metav1.ObjectMeta, spec TargetSnapshotSpec) *TargetSnapshot {
	return &TargetSnapshot{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.Identifier(),
			Kind:       TargetSnapshotKind,
		},
		ObjectMeta: meta,
		Spec:       spec,
	}
}

// BuildEmptyTargetSnapshot returns an empty TargetSnapshot
func BuildEmptyTargetSnapshot() *TargetSnapshot {
	return &TargetSnapshot{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.Identifier(),
			Kind:       TargetSnapshotKind,
		},
	}
}

// GetTargetSnapshotFromFile is a helper for tests to use the
// examples and validate them in unit tests
func GetTargetSnapshotFromFile(path string) (*TargetSnapshot, error) {
	addToScheme := AddToScheme
	obj := &TargetSnapshot{}
	gvk := SchemeGroupVersion.WithKind(reflect.TypeOf(obj).Name())
	// build object from file
	if err := testhelper.GetKRMResource(path, obj, gvk, addToScheme); err != nil {
		return nil, err
	}
	return obj, nil
}

// GetShaSum calculates the shasum of the confgiSpec
func (r *TargetSnapshotSpec) GetShaSum(ctx context.Context) [20]byte {
	log := log.FromContext(ctx)
	appliedSpec, err := json.Marshal(r)
	if err != nil {
		log.Error("cannot marshal appliedTargetSnapshot", "error", err)
		return [20]byte{}
	}
	return sha1.Sum(appliedSpec)
}

// ConvertTargetSnapshotFieldSelector is the schema conversion function for normalizing the FieldSelector for TargetSnapshot
func ConvertTargetSnapshotFieldSelector(label, value string) (internalLabel, internalValue string, err error) {
	switch label {
	case "metadata.name":
		return label, value, nil
	case "metadata.namespace":
		return label, value, nil
	default:
		return "", "", fmt.Errorf("%q is not a known field selector", label)
	}
}

func (r *TargetSnapshot) CalculateHash() ([sha1.Size]byte, error) {
	// Convert the struct to JSON
	jsonData, err := json.Marshal(r)
	if err != nil {
		return [sha1.Size]byte{}, err
	}

	// Calculate SHA-1 hash
	return sha1.Sum(jsonData), nil
}

func (r *TargetSnapshot) DeepObjectCopy() client.Object {
	return r.DeepCopy()
}
