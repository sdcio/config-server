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
	"net/url"

	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/rest"
)

var _ resource.ArbitrarySubResource = &TargetRunningConfig{}

func (TargetRunningConfig) SubResourceName() string {
	return "runningconfig"
}

func (TargetRunningConfig) New() runtime.Object {
	return &TargetRunningConfig{} // returns parent type — GET returns the full Target
}

func (TargetRunningConfig) NewStorage(scheme *runtime.Scheme, parentStorage rest.Storage) (rest.Storage, error) {
	return &targetRunningConfigREST{
		parentStore: parentStorage,
	}, nil
}

var _ resource.ArbitrarySubResourceWithOptions = &TargetRunningConfig{}

func (TargetRunningConfig) NewGetOptions() runtime.Object {
	return &TargetRunningConfigOptions{}
}

var _ resource.ArbitrarySubResourceWithOptionsConverter = &TargetRunningConfig{}

func (TargetRunningConfig) ConvertFromURLValues() func(a, b interface{}, scope conversion.Scope) error {
	return func(a, b interface{}, scope conversion.Scope) error {
		values := a.(*url.Values)
		out := b.(*TargetRunningConfigOptions)
		out.Path = values.Get("path")
		out.Format = values.Get("format")
		return nil
	}
}

// targetRunningREST implements rest.Storage + rest.Getter
type targetRunningConfigREST struct {
	parentStore rest.Storage
}

func (r *targetRunningConfigREST) New() runtime.Object {
	return &TargetRunningConfig{}
}

func (r *targetRunningConfigREST) Destroy() {}

func (r *targetRunningConfigREST) NewGetOptions() (runtime.Object, bool, string) {
	// Returns: (options object, decode from body?, single query param name)
	return &TargetRunningConfigOptions{}, false, ""
}

func (r *targetRunningConfigREST) Get(ctx context.Context, name string, options runtime.Object) (runtime.Object, error) {
	opts, ok := options.(*TargetRunningConfigOptions)
	if !ok {
		return nil, apierrors.NewBadRequest(
			fmt.Sprintf("expected TargetRunningConfigOptions, got %T", options))
	}

	// Get the parent Target from the parent store
	// Get the parent Target from the parent store
	getter, ok := r.parentStore.(rest.Getter)
	if !ok {
		return nil, apierrors.NewInternalError(
			fmt.Errorf("parent store %T does not implement rest.Getter", r.parentStore))
	}
	obj, err := getter.Get(ctx, name, &metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	target, ok := obj.(*Target)
	if !ok {
		return nil, apierrors.NewInternalError(
			fmt.Errorf("expected *Target, got %T", obj))
	}

	return target.GetRunningConfig(ctx, opts)

}
