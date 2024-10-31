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
				Repositories: []*SchemaSpecRepository{
					{Schema: SchemaSpecSchema{Models: nil, Excludes: nil, Includes: nil}},
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
				Repositories: []*SchemaSpecRepository{
					{Schema: SchemaSpecSchema{Models: []string{"a", "b"}, Excludes: []string{"a"}, Includes: []string{"a"}}},
				},
			},
			output: SchemaSpecSchema{
				Models:   []string{"dummy/a", "dummy/b"},
				Includes: []string{"dummy/a"},
				Excludes: []string{"a"},
			},
		},
		"Multiple": {
			basePath: "dummy",
			input: &SchemaSpec{
				Repositories: []*SchemaSpecRepository{
					{Schema: SchemaSpecSchema{Models: []string{"a", "b"}, Excludes: []string{"a"}, Includes: []string{"a"}}},
					{Schema: SchemaSpecSchema{Models: []string{"c", "b"}, Excludes: []string{"a"}, Includes: []string{"a"}}},
				},
			},
			output: SchemaSpecSchema{
				Models:   []string{"dummy/a", "dummy/b", "dummy/c"},
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

func TestExampleSchemas(t *testing.T) {
	cases := map[string]struct {
		path        string
		expectedErr error
	}{
		"JuniperEx": {
			path:        "../../../example/schemas/schema-juniper-ex-23.2R1.yaml",
			expectedErr: nil,
		},
		"JuniperMx": {
			path:        "../../../example/schemas/schema-juniper-mx-23.2R1.yaml",
			expectedErr: nil,
		},
		"JuniperNfx": {
			path:        "../../../example/schemas/schema-juniper-nfx-23.2R1.yaml",
			expectedErr: nil,
		},
		"JuniperQfx": {
			path:        "../../../example/schemas/schema-juniper-qfx-23.2R1.yaml",
			expectedErr: nil,
		},
		"NokiaSrl": {
			path:        "../../../example/schemas/schema-nokia-srl-23.10.1.yaml",
			expectedErr: nil,
		},
		"NokiaSros": {
			path:        "../../../example/schemas/schema-nokia-sros-23.10.yaml",
			expectedErr: nil,
		},
		"Arista": {
			path:        "../../../example/schemas/schema-arista-4.31.2.f.yaml",
			expectedErr: nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := GetSchemaFromFile(tc.path)
			if err != nil {
				if tc.expectedErr == nil {
					t.Errorf("unexpected error\n%s", err.Error())
				}
				return
			}
			if tc.expectedErr != nil {
				t.Errorf("%s expecting an error, got nil", name)
			}
		})
	}
}
