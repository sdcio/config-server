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

package testhelper

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestGetObject(t *testing.T) {

	cases := map[string]struct {
		path        string
		addToScheme func(s *runtime.Scheme) error
		gv          schema.GroupVersion
		obj         runtime.Object
	}{
		/*
			"Config": {
				path:        "../../example/config/config.yaml",
				addToScheme: configv1alpha1.AddToScheme,
				gv:         configv1alpha1.ConfigGroupVersion,
				obj:         &configv1alpha1.Config{},
			},
		*/
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			gvk := tc.gv.WithKind(reflect.TypeOf(tc.obj).Name())
			obj := tc.obj
			if err := GetKRMResource(tc.path, obj, gvk, tc.addToScheme); err != nil {
				t.Errorf("%s unexpected error: %s", name, err.Error())
				return
			}
		})
	}
}
