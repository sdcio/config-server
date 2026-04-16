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

	"github.com/henderiw/logger/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ApplyConfigFn builds a typed apply configuration for the given object.
// When finalizers is non-empty, they should be set on the apply config.
type ApplyConfigFn func(name, namespace string, finalizers ...string) runtime.ApplyConfiguration

type Finalizer interface {
	AddFinalizer(ctx context.Context, obj runtime.ApplyConfiguration) error
	RemoveFinalizer(ctx context.Context, obj runtime.ApplyConfiguration) error
}

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

func (r *APIFinalizer) AddFinalizer(ctx context.Context, obj client.Object) error {
	if FinalizerExists(obj, r.finalizer) {
		return nil
	}
	log.FromContext(ctx).Info("SSA applying finalizer", "fieldManager", r.fieldManager, "finalizer", r.finalizer)
	applyConfig := r.buildApply(obj.GetName(), obj.GetNamespace(), r.finalizer)

	log.FromContext(ctx).Info("SSA applying finalizer", "fieldManager", r.fieldManager, "applyConfig", applyConfig)

	return r.client.Apply(ctx, applyConfig, &client.ApplyOptions{
		FieldManager: r.fieldManager,
	})
}

func (r *APIFinalizer) RemoveFinalizer(ctx context.Context, obj client.Object) error {
	if !FinalizerExists(obj, r.finalizer) {
		return nil
	}
	// Apply with no finalizers — SSA removes only what this field manager owns
	applyConfig := r.buildApply(obj.GetName(), obj.GetNamespace())

	log.FromContext(ctx).Info("SSA removing finalizer", "fieldManager", r.fieldManager, "applyConfig", applyConfig)

	return r.client.Apply(ctx, applyConfig, &client.ApplyOptions{
		FieldManager: r.fieldManager,
	})
}

func FinalizerExists(o metav1.Object, finalizer string) bool {
	for _, e := range o.GetFinalizers() {
		if e == finalizer {
			return true
		}
	}
	return false
}
