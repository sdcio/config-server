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
	"fmt"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	// take a snapshot of the current object
	origObj := obj.DeepCopyObject()

	// Again assert to client.Object to manipulate metadata and finalizers
	copiedObj, ok := origObj.(client.Object)
	if !ok {
		return fmt.Errorf("deep copied object does not implement client.Object")
	}

	patch := client.MergeFrom(copiedObj)

	if FinalizerExists(obj, r.finalizer) {
		return nil
	}
	AddFinalizer(obj, r.finalizer)
	return errors.Wrap(r.Update(ctx, obj, patch), errUpdateObject)
}

func (r *APIFinalizer) Update(ctx context.Context, obj client.Object, patch client.Patch) error {
	return r.client.Patch(ctx, obj, patch, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: r.reconcilerName,
		},
	})
}

func (r *APIFinalizer) RemoveFinalizer(ctx context.Context, obj client.Object) error {
	// take a snapshot of the current object
	origObj := obj.DeepCopyObject()

	// Again assert to client.Object to manipulate metadata and finalizers
	copiedObj, ok := origObj.(client.Object)
	if !ok {
		return fmt.Errorf("deep copied object does not implement client.Object")
	}
	patch := client.MergeFrom(copiedObj)

	if !FinalizerExists(obj, r.finalizer) {
		return nil
	}
	RemoveFinalizer(obj, r.finalizer)
	return errors.Wrap(r.Update(ctx, obj, patch), errUpdateObject)
}

// AddFinalizer to the supplied k8s object's metadata.
func AddFinalizer(o metav1.Object, finalizer string) {
	f := o.GetFinalizers()
	for _, e := range f {
		if e == finalizer {
			return
		}
	}
	o.SetFinalizers(append(f, finalizer))
}

// RemoveFinalizer from the supplied k8s object's metadata.
func RemoveFinalizer(o metav1.Object, finalizer string) {
	f := o.GetFinalizers()
	for i, e := range f {
		if e == finalizer {
			f = append(f[:i], f[i+1:]...)
		}
	}
	o.SetFinalizers(f)
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
