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

	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/rest"
)

var _ resource.ArbitrarySubResource = &TargetConfigBlame{}

func (TargetConfigBlame) SubResourceName() string { return "configblame" }
func (TargetConfigBlame) New() runtime.Object     { return &TargetConfigBlame{} }
func (TargetConfigBlame) NewStorage(_ *runtime.Scheme, _ rest.Storage) (rest.Storage, error) {
	return nil, fmt.Errorf("not implemented on versioned type")
}
