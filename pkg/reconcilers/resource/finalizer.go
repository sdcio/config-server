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

package resource

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/henderiw/logger/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ApplyConfigFn builds a typed apply configuration for the given object.
// When finalizers is non-empty, they should be set on the apply config.
type ApplyConfigFn func(name, namespace string, finalizers ...string) runtime.ApplyConfiguration

type Finalizer interface {
	AddFinalizer(ctx context.Context, obj client.Object) error
	RemoveFinalizer(ctx context.Context, obj client.Object) error
}

var _ Finalizer = &APIFinalizer{}

type APIFinalizer struct {
	client       client.Client
	fieldManager string
	finalizer    string
	buildApply   ApplyConfigFn
}

func NewAPIFinalizer(c client.Client, finalizer, fieldManager string, buildApply ApplyConfigFn) *APIFinalizer {
	return &APIFinalizer{
		client:       c,
		finalizer:    finalizer,
		fieldManager: fieldManager,
		buildApply:   buildApply,
	}
}

// AddFinalizer adds the finalizer with SSA, owned by fieldManager.
func (r *APIFinalizer) AddFinalizer(ctx context.Context, obj client.Object) error {
	if FinalizerExists(obj, r.finalizer) {
		return nil
	}
	applyConfig := r.buildApply(obj.GetName(), obj.GetNamespace(), r.finalizer)

	log.FromContext(ctx).Info("SSA applying finalizer",
		"fieldManager", r.fieldManager, "finalizer", r.finalizer, "applyConfig", applyConfig)

	return r.client.Apply(ctx, applyConfig, &client.ApplyOptions{
		FieldManager: r.fieldManager,
	})
}

// RemoveFinalizer removes the finalizer with a JSON patch, not with SSA.
//
// An empty apply only drops this manager's ownership of the entry: when
// another manager owns or co-owns it, the finalizer stays and the apply still
// succeeds, so the object never goes away. That is the case for a finalizer
// added with a merge patch (an Update operation) by an earlier build, or by
// any other client. Removing the entry from the list drops it, and every
// manager's ownership of it, whoever added it.
//
// The test op guards the index: when obj is stale (the list changed in
// between), the patch fails with 422 and the caller retries on a fresh read.
func (r *APIFinalizer) RemoveFinalizer(ctx context.Context, obj client.Object) error {
	idx := finalizerIndex(obj, r.finalizer)
	if idx < 0 {
		return nil
	}
	path := fmt.Sprintf("/metadata/finalizers/%d", idx)
	patch, err := json.Marshal([]map[string]any{
		{"op": "test", "path": path, "value": r.finalizer},
		{"op": "remove", "path": path},
	})
	if err != nil {
		return err
	}

	log.FromContext(ctx).Info("removing finalizer",
		"fieldManager", r.fieldManager, "finalizer", r.finalizer, "index", idx)

	// patch a copy: the response must not overwrite the caller's object
	return r.client.Patch(ctx, obj.DeepCopyObject().(client.Object),
		client.RawPatch(types.JSONPatchType, patch),
		client.FieldOwner(r.fieldManager))
}

func FinalizerExists(o metav1.Object, finalizer string) bool {
	return finalizerIndex(o, finalizer) >= 0
}

func finalizerIndex(o metav1.Object, finalizer string) int {
	for i, e := range o.GetFinalizers() {
		if e == finalizer {
			return i
		}
	}
	return -1
}