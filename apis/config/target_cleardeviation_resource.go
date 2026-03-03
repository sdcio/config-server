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

	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ resource.ArbitrarySubResource = &targetClearDeviationSubResource{}

type targetClearDeviationSubResource struct {
	client client.Client
}

func NewTargetClearDeviationSubResource(c client.Client) *targetClearDeviationSubResource {
	return &targetClearDeviationSubResource{client: c}
}

func (r *targetClearDeviationSubResource) SubResourceName() string {
	return "cleardeviation"
}

func (r *targetClearDeviationSubResource) New() runtime.Object {
	return &TargetClearDeviation{}
}

func (r *targetClearDeviationSubResource) NewStorage(scheme *runtime.Scheme, parentStorage rest.Storage) (rest.Storage, error) {
	return &targetClearDeviationREST{
		parentStore: parentStorage,
		client:      r.client,
	}, nil
}

// --- REST handler ---

type targetClearDeviationREST struct {
	parentStore rest.Storage
	client      client.Client
}

func (r *targetClearDeviationREST) New() runtime.Object {
	return &TargetClearDeviation{}
}

func (r *targetClearDeviationREST) Destroy() {}

// Create handles POST /apis/.../targets/{name}/cleardeviation
func (r *targetClearDeviationREST) Create(
	ctx context.Context,
	obj runtime.Object,
	createValidation rest.ValidateObjectFunc,
	options *metav1.CreateOptions,
) (runtime.Object, error) {
	req, ok := obj.(*TargetClearDeviation)
	if !ok {
		return nil, apierrors.NewBadRequest(
			fmt.Sprintf("expected *TargetClearDeviation, got %T", obj))
	}

	// Get the parent target
	getter, ok := r.parentStore.(rest.Getter)
	if !ok {
		return nil, apierrors.NewInternalError(
			fmt.Errorf("parent store %T does not implement rest.Getter", r.parentStore))
	}

	// Get the parent target to validate it exists and is ready
	parentObj, err := getter.Get(ctx, req.Name, &metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	target, ok := parentObj.(*Target)
	if !ok {
		return nil, apierrors.NewInternalError(
			fmt.Errorf("expected *Target, got %T", parentObj))
	}

	return target.ClearDeviations(ctx, r.client, req)

}
