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

package config

import (
	"context"
	"crypto/sha1"
	"encoding/json"

	"github.com/henderiw/logger/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

func (r *TargetSnapshot) GetNamespacedName() types.NamespacedName {
	return types.NamespacedName{Name: r.Name, Namespace: r.Namespace}
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

func BuildEmptyTargetSnapshot() *TargetSnapshot {
	return &TargetSnapshot{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.Identifier(),
			Kind:       TargetSnapshotKind,
		},
	}
}

type RecoveryTargetSnapshot struct {
	TargetSnapshot TargetSnapshot
}

func (r *TargetSnapshotSpec) GetShaSum(ctx context.Context) [20]byte {
	log := log.FromContext(ctx)
	appliedSpec, err := json.Marshal(r)
	if err != nil {
		log.Error("cannot marshal appliedConfig", "error", err)
		return [20]byte{}
	}
	return sha1.Sum(appliedSpec)
}

func (r *TargetSnapshot) GetOwnerReference() metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: SchemeGroupVersion.Identifier(),
		Kind:       TargetSnapshotKind,
		Name:       r.Name,
		UID:        r.UID,
		Controller: ptr.To(true),
	}
}
