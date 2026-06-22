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

package openapi

import (
	"reflect"
	"testing"

	"k8s.io/kube-openapi/pkg/common"
	"k8s.io/kube-openapi/pkg/validation/spec"

	generated "github.com/sdcio/config-server/pkg/generated/openapi"
)

func nopRef(string) spec.Ref { return spec.Ref{} }

func TestGetOpenAPIDefinitions_AddsPreserveUnknownFieldsToRawExtension(t *testing.T) {
	defs := GetOpenAPIDefinitions(nopRef)

	d, ok := defs[rawExtensionModelName]
	if !ok {
		t.Fatalf("expected %q in OpenAPI definitions, but it was missing", rawExtensionModelName)
	}

	v, ok := d.Schema.Extensions[preserveUnknownFieldsExtension]
	if !ok {
		t.Fatalf("expected extension %q on RawExtension schema, but it was missing", preserveUnknownFieldsExtension)
	}
	b, ok := v.(bool)
	if !ok {
		t.Fatalf("expected extension %q to be a bool (must serialize as JSON true, not the string \"true\"), got %T (%v)",
			preserveUnknownFieldsExtension, v, v)
	}
	if !b {
		t.Fatalf("expected extension %q to be true, got false", preserveUnknownFieldsExtension)
	}
}

// The patch must only add the one extension; everything else (description,
// type, dependencies, other extensions) must be untouched.
func TestGetOpenAPIDefinitions_PatchIsAdditive(t *testing.T) {
	original := generated.GetOpenAPIDefinitions(nopRef)[rawExtensionModelName]
	patched := GetOpenAPIDefinitions(nopRef)[rawExtensionModelName]

	if !reflect.DeepEqual(original.Schema.SchemaProps, patched.Schema.SchemaProps) {
		t.Errorf("SchemaProps changed by patch:\noriginal: %+v\npatched:  %+v",
			original.Schema.SchemaProps, patched.Schema.SchemaProps)
	}
	if !reflect.DeepEqual(original.Schema.SwaggerSchemaProps, patched.Schema.SwaggerSchemaProps) {
		t.Errorf("SwaggerSchemaProps changed by patch:\noriginal: %+v\npatched:  %+v",
			original.Schema.SwaggerSchemaProps, patched.Schema.SwaggerSchemaProps)
	}
	if !reflect.DeepEqual(original.Dependencies, patched.Dependencies) {
		t.Errorf("Dependencies changed by patch:\noriginal: %v\npatched:  %v",
			original.Dependencies, patched.Dependencies)
	}

	wantExtKeys := map[string]struct{}{}
	for k := range original.Schema.Extensions {
		wantExtKeys[k] = struct{}{}
	}
	wantExtKeys[preserveUnknownFieldsExtension] = struct{}{}

	if got, want := len(patched.Schema.Extensions), len(wantExtKeys); got != want {
		t.Fatalf("patched RawExtension extensions count = %d, want %d (keys want=%v, got=%v)",
			got, want, sortedKeys(wantExtKeys), patched.Schema.Extensions)
	}
	for k := range wantExtKeys {
		if _, ok := patched.Schema.Extensions[k]; !ok {
			t.Errorf("expected extension key %q to be present after patch", k)
		}
	}
}

// If RawExtension isn't registered for some reason, patching is a no-op
func TestPatchRawExtension_MissingEntryIsNoop(t *testing.T) {
	empty := map[string]common.OpenAPIDefinition{}
	patchRawExtension(empty)
	if _, ok := empty[rawExtensionModelName]; ok {
		t.Errorf("patchRawExtension must not insert RawExtension into an empty map")
	}
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
