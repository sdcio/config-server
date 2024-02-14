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
	"fmt"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/watch"
)

func (r *strategy) BeginUpdate(ctx context.Context) error { return nil }

func (r *strategy) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
}

func (r *strategy) AllowCreateOnUpdate() bool { return false }

func (r *strategy) AllowUnconditionalUpdate() bool { return false }

func (r *strategy) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	var allErrs field.ErrorList

	newaccessor, err := meta.Accessor(obj)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath(""),
			obj,
			err.Error(),
		))
		return allErrs
	}
	newKey, err := getTargetKey(newaccessor.GetLabels())
	if err != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("metadata.labels"),
			obj,
			err.Error(),
		))
	}
	oldaccessor, err := meta.Accessor(old)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath(""),
			obj,
			err.Error(),
		))
		return allErrs
	}
	oldKey, err := getTargetKey(oldaccessor.GetLabels())
	if err != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("metadata.labels"),
			obj,
			err.Error(),
		))
	}
	if len(allErrs) != 0 {
		return allErrs
	}
	if oldKey.String() != newKey.String() {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("metadata.labels"),
			obj,
			fmt.Errorf("target keys cannot change: oldKey: %s, newKey %s", oldKey.String(), newKey.String()).Error(),
		))
	}

	return allErrs
}

func (r *strategy) Update(ctx context.Context, key types.NamespacedName, obj runtime.Object) error {
	if err := r.store.Update(ctx, storebackend.KeyFromNSN(key), obj); err != nil {
		return apierrors.NewInternalError(err)
	}
	r.notifyWatcher(ctx, watch.Event{
		Type:   watch.Modified,
		Object: obj,
	})
	return nil
}

func (r *strategy) WarningsOnUpdate(ctx context.Context, obj, old runtime.Object) []string {
	return nil
}
