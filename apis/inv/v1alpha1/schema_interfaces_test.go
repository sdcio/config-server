/*
Copyright 2023 The Nephio Authors.

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
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGetNewSchemaBase(t *testing.T) {
	cases := map[string]struct {
		basePath string
		input    *SchemaSpec
		output   SchemaSpecSchema
	}{
		"Nil": {
			basePath: "dummy",
			input: &SchemaSpec{
				Schema: SchemaSpecSchema{
					Models:   nil,
					Excludes: nil,
					Includes: nil,
				},
			},
			output: SchemaSpecSchema{
				Models:   []string{"dummy"},
				Includes: []string{},
				Excludes: []string{},
			},
		},
		"Init": {
			basePath: "dummy",
			input: &SchemaSpec{
				Schema: SchemaSpecSchema{
					Models:   []string{"a", "b"},
					Excludes: []string{"a"},
					Includes: []string{"a"},
				},
			},
			output: SchemaSpecSchema{
				Models:   []string{"dummy/a", "dummy/b"},
				Includes: []string{"dummy/a"},
				Excludes: []string{"a"},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			newSchemaSpec := tc.input.GetNewSchemaBase(tc.basePath)

			if diff := cmp.Diff(newSchemaSpec, tc.output); diff != "" {
				t.Errorf("TestGetNewSchemaBase: -want, +got:\n%s", diff)
			}
		})
	}
}
