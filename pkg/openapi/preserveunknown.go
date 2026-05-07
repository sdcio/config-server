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

// Package openapi marks runtime.RawExtension as preserve-unknown-fields in
// the served OpenAPI, so clients doing strict structural validation accept
// arbitrary payloads in fields typed as RawExtension. openapi-gen doesn't
// emit that extension itself.
package openapi

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kube-openapi/pkg/common"
	"k8s.io/kube-openapi/pkg/validation/spec"

	generated "github.com/sdcio/config-server/pkg/generated/openapi"
)

const preserveUnknownFieldsExtension = "x-kubernetes-preserve-unknown-fields"

// Use the type's own model name so we stay in lockstep with whatever key the
// generator picked for the RawExtension entry in the definitions map.
var rawExtensionModelName = runtime.RawExtension{}.OpenAPIModelName()

// GetOpenAPIDefinitions returns the generated definitions with
// x-kubernetes-preserve-unknown-fields: true added to runtime.RawExtension.
func GetOpenAPIDefinitions(ref common.ReferenceCallback) map[string]common.OpenAPIDefinition {
	defs := generated.GetOpenAPIDefinitions(ref)
	patchRawExtension(defs)
	return defs
}

func patchRawExtension(defs map[string]common.OpenAPIDefinition) {
	d, ok := defs[rawExtensionModelName]
	if !ok {
		return
	}
	if d.Schema.Extensions == nil {
		d.Schema.Extensions = spec.Extensions{}
	}
	d.Schema.Extensions[preserveUnknownFieldsExtension] = true
	defs[rawExtensionModelName] = d
}
