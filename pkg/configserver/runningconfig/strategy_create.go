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

package runningconfig

import (
	"context"

	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func (r *strategy) BeginCreate(ctx context.Context) error {
	return apierrors.NewMethodNotSupported(configv1alpha1.Resource(configv1alpha1.RunningConfigPlural), "create")
}

func (r *strategy) PrepareForCreate(ctx context.Context, obj runtime.Object) {
}

func (r *strategy) Validate(ctx context.Context, obj runtime.Object) field.ErrorList {
	return field.ErrorList{}
}

func (r *strategy) Create(ctx context.Context, key types.NamespacedName, obj runtime.Object, dryrun bool) (runtime.Object, error) {
	return obj, apierrors.NewMethodNotSupported(configv1alpha1.Resource(configv1alpha1.RunningConfigPlural), "create")
}

func (r *strategy) WarningsOnCreate(ctx context.Context, obj runtime.Object) []string {
	return nil
}
