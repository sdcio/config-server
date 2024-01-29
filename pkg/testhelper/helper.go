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
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
)

func GetKRMResource(path string, obj runtime.Object, gvk schema.GroupVersionKind, addToScheme func(s *runtime.Scheme) error) error {
	// build scheme
	scheme, err := getScheme(runtime.SchemeBuilder{addToScheme})
	if err != nil {
		return err
	}
	c := &codec{
		scheme: scheme,
		gvk:    gvk,
	}

	if err := c.getObject(path, obj); err != nil {
		return err
	}
	return nil
}

func getScheme(sb runtime.SchemeBuilder) (*runtime.Scheme, error) {
	scheme := runtime.NewScheme()
	for _, addscheme := range sb {
		if err := addscheme(scheme); err != nil {
			return nil, err
		}
	}
	return scheme, nil
}

type codec struct {
	scheme *runtime.Scheme
	gvk    schema.GroupVersionKind
}

func (r *codec) getObject(path string, obj runtime.Object) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	s := json.NewYAMLSerializer(json.DefaultMetaFactory, r.scheme, r.scheme)
	_, _, err = s.Decode(b, &schema.GroupVersionKind{}, obj)
	return err
}
