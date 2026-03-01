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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Finalizer manages the lifecycle of the finalizer on a k8s object
type Finalizer interface {
	AddFinalizer(ctx context.Context, obj client.Object) error
	RemoveFinalizer(ctx context.Context, obj client.Object) error
}

// An APIFinalizer manages the lifecycle of a finalizer on a k8s object.
type APIFinalizer struct {
	client         client.Client
	reconcilerName string
	finalizer      string
}

// NewAPIFinalizer returns a new APIFinalizer.
func NewAPIFinalizer(c client.Client, finalizer, reconcilerName string) *APIFinalizer {
	return &APIFinalizer{client: c, finalizer: finalizer, reconcilerName: reconcilerName}
}

// AddFinalizer to the supplied Managed resource.
func (r *APIFinalizer) AddFinalizer(ctx context.Context, obj client.Object) error {
	if FinalizerExists(obj, r.finalizer) {
		return nil
	}
	patch, err := r.finalizerPatch(obj, []string{r.finalizer})
	if err != nil {
		return fmt.Errorf("cannot build finalizer patch: %w", err)
	}
	return r.client.Patch(ctx, obj, client.RawPatch(types.ApplyPatchType, patch), client.ForceOwnership, &client.PatchOptions{
		FieldManager: r.reconcilerName,
	})
}

func (r *APIFinalizer) RemoveFinalizer(ctx context.Context, obj client.Object) error {
	if !FinalizerExists(obj, r.finalizer) {
		return nil
	}
	patch, err := r.finalizerPatch(obj, nil)
	if err != nil {
		return fmt.Errorf("cannot build finalizer patch: %w", err)
	}
	return r.client.Patch(ctx, obj, client.RawPatch(types.ApplyPatchType, patch), client.ForceOwnership, &client.PatchOptions{
		FieldManager: r.reconcilerName,
	})
}

// finalizerPatch builds a minimal SSA patch containing only apiVersion, kind, metadata.name,
// metadata.namespace, and metadata.finalizers.
func (r *APIFinalizer) finalizerPatch(obj client.Object, finalizers []string) ([]byte, error) {
	gvk := obj.GetObjectKind().GroupVersionKind()
	if gvk.Empty() {
		return nil, fmt.Errorf("object has no GVK set")
	}

	patch := map[string]interface{}{
		"apiVersion": gvk.GroupVersion().String(),
		"kind":       gvk.Kind,
		"metadata": map[string]interface{}{
			"name":       obj.GetName(),
			"namespace":  obj.GetNamespace(),
			"finalizers": finalizers,
		},
	}
	return json.Marshal(patch)
}

// FinalizerExists checks whether a certain finalizer is already set
// on the aupplied object
func FinalizerExists(o metav1.Object, finalizer string) bool {
	f := o.GetFinalizers()
	for _, e := range f {
		if e == finalizer {
			return true
		}
	}
	return false
}
