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
)

const (
	SubResource_SensitiveConfigBlame = "sensitiveconfigblame"
)

var _ resource.ArbitrarySubResource = &TargetSensitiveConfigBlame{}

func (TargetSensitiveConfigBlame) SubResourceName() string {
	return SubResource_ConfigBlame
}

func (TargetSensitiveConfigBlame) New() runtime.Object {
	return &TargetSensitiveConfigBlame{} // returns parent type — GET returns the full Target
}

func (TargetSensitiveConfigBlame) NewStorage(scheme *runtime.Scheme, parentStorage rest.Storage) (rest.Storage, error) {
	return &targetSensitiveConfigBlameREST{
		parentStore: parentStorage,
	}, nil
}

// targetBlameREST implements rest.Storage + rest.Getter
type targetSensitiveConfigBlameREST struct {
	parentStore rest.Storage
}

func (r *targetSensitiveConfigBlameREST) New() runtime.Object {
	return &TargetSensitiveConfigBlame{}
}

func (r *targetSensitiveConfigBlameREST) Destroy() {}

func (r *targetSensitiveConfigBlameREST) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	// Get the parent Target from the parent store
	getter, ok := r.parentStore.(rest.Getter)
	if !ok {
		return nil, apierrors.NewInternalError(
			fmt.Errorf("parent store %T does not implement rest.Getter", r.parentStore))
	}

	obj, err := getter.Get(ctx, name, options)
	if err != nil {
		return nil, err
	}
	target, ok := obj.(*Target)
	if !ok {
		return nil, apierrors.NewInternalError(
			fmt.Errorf("expected *Target, got %T", obj))
	}

	return target.GetConfigBlame(ctx, true)
}
