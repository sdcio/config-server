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

package config

import (
	"context"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/watch"
)

func (r *strategy) BeginCreate(ctx context.Context) error { return nil }

func (r *strategy) PrepareForCreate(ctx context.Context, obj runtime.Object) {
}

func (r *strategy) Validate(ctx context.Context, obj runtime.Object) field.ErrorList {
	var allErrs field.ErrorList

	accessor, err := meta.Accessor(obj)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath(""),
			obj,
			err.Error(),
		))
		return allErrs
	}
	if _, err := getTargetKey(accessor.GetLabels()); err != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("metadata.labels"),
			obj,
			err.Error(),
		))
	}

	return allErrs
}

func (r *strategy) Create(ctx context.Context, key types.NamespacedName, obj runtime.Object) error {
	if err := r.store.Create(ctx, storebackend.KeyFromNSN(key), obj); err != nil {
		return apierrors.NewInternalError(err)
	}
	r.notifyWatcher(ctx, watch.Event{
		Type:   watch.Added,
		Object: obj,
	})
	return nil
}

func (r *strategy) WarningsOnCreate(ctx context.Context, obj runtime.Object) []string {
	return nil
}
