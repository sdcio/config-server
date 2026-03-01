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

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Finalizer interface {
	AddFinalizer(ctx context.Context, obj client.Object) error
	RemoveFinalizer(ctx context.Context, obj client.Object) error
}

type APIFinalizer struct {
	client         client.Client
	reconcilerName string
	finalizer      string
}

func NewAPIFinalizer(c client.Client, finalizer, reconcilerName string) *APIFinalizer {
	return &APIFinalizer{client: c, finalizer: finalizer, reconcilerName: reconcilerName}
}

func (r *APIFinalizer) AddFinalizer(ctx context.Context, obj client.Object) error {
	if FinalizerExists(obj, r.finalizer) {
		return nil
	}
	addFinalizer(obj, r.finalizer)
	return errors.Wrap(r.client.Update(ctx, obj), errUpdateObject)
}

func (r *APIFinalizer) RemoveFinalizer(ctx context.Context, obj client.Object) error {
	if !FinalizerExists(obj, r.finalizer) {
		return nil
	}
	removeFinalizer(obj, r.finalizer)
	return errors.Wrap(r.client.Update(ctx, obj), errUpdateObject)
}

func addFinalizer(o metav1.Object, finalizer string) {
	f := o.GetFinalizers()
	for _, e := range f {
		if e == finalizer {
			return
		}
	}
	o.SetFinalizers(append(f, finalizer))
}

func removeFinalizer(o metav1.Object, finalizer string) {
	var result []string
	for _, e := range o.GetFinalizers() {
		if e != finalizer {
			result = append(result, e)
		}
	}
	o.SetFinalizers(result)
}

func FinalizerExists(o metav1.Object, finalizer string) bool {
	for _, e := range o.GetFinalizers() {
		if e == finalizer {
			return true
		}
	}
	return false
}