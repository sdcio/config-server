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

package v1alpha1

import (
	"fmt"
	"net/url"

	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	conversion "k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/rest"
)

var _ resource.ArbitrarySubResource = &TargetRunningConfig{}

func (TargetRunningConfig) SubResourceName() string { return "runningconfig" }
func (TargetRunningConfig) New() runtime.Object     { return &TargetRunningConfig{} }
func (TargetRunningConfig) NewStorage(_ *runtime.Scheme, _ rest.Storage) (rest.Storage, error) {
	return nil, fmt.Errorf("not implemented on versioned type")
}

var _ resource.ArbitrarySubResourceWithOptions = &TargetRunningConfig{}

func (TargetRunningConfig) NewGetOptions() runtime.Object {
	return &TargetRunningOptions{}
}

var _ resource.ArbitrarySubResourceWithOptionsConverter = &TargetRunningConfig{}

func (TargetRunningConfig) ConvertFromURLValues() func(a, b interface{}, scope conversion.Scope) error {
	return func(a, b interface{}, scope conversion.Scope) error {
		values := a.(*url.Values)
		out := b.(*TargetRunningOptions)
		out.Path = values.Get("path")
		out.Format = values.Get("format")
		return nil
	}
}
