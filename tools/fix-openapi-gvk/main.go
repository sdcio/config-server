/*
Copyright 2026 Nokia.

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

// fix-openapi-gvk injects x-kubernetes-group-version-kind extensions into
// zz_generated.openapi.go for top-level resource types. openapi-gen omits
// these extensions, but NewTypeConverter requires them to register types by GVK.
//
// VendorExtensible is an embedded field of spec.Schema, so it must be injected
// INSIDE spec.Schema{} before its closing brace, not at the OpenAPIDefinition level.
//
// Only types whose properties include "kind", "apiVersion" and "metadata" are
// considered top-level resources and get the extension injected.
// List types (names ending in "List") are excluded.
// Only functions matching --prefix are processed, so types from other API groups
// are left untouched. The API version is derived from the prefix automatically.
//
// Usage:
//
//	go run ./tools/fix-openapi-gvk/main.go \
//	  --file pkg/generated/openapi/zz_generated.openapi.go \
//	  --group config.sdcio.dev \
//	  --prefix config_server_apis_config_v1alpha1
package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	filePath = flag.String("file", "", "path to zz_generated.openapi.go")
	group    = flag.String("group", "", "API group, e.g. config.sdcio.dev")
	prefix   = flag.String("prefix", "", "underscore-delimited package prefix from function names, e.g. config_server_apis_config_v1alpha1")
)

// versionRe matches Kubernetes-style API versions like v1, v1alpha1, v1beta2.
var versionRe = regexp.MustCompile(`^v\d+(?:(?:alpha|beta)\d+)?$`)

// extractVersion returns the API version from the prefix by finding the last
// segment that matches a k8s version pattern (e.g. v1alpha1, v1beta1, v1).
func extractVersion(pfx string) (string, error) {
	parts := strings.Split(pfx, "_")
	for i := len(parts) - 1; i >= 0; i-- {
		if versionRe.MatchString(parts[i]) {
			return parts[i], nil
		}
	}
	return "", fmt.Errorf("no API version found in prefix %q (expected segment like v1, v1alpha1, v1beta2)", pfx)
}

func vendorBlock(g, k, v string) string {
	return fmt.Sprintf(
		"\t\t\tVendorExtensible: spec.VendorExtensible{\n"+
			"\t\t\t\tExtensions: spec.Extensions{\n"+
			"\t\t\t\t\t\"x-kubernetes-group-version-kind\": []interface{}{\n"+
			"\t\t\t\t\t\tmap[string]interface{}{\"group\": %q, \"kind\": %q, \"version\": %q},\n"+
			"\t\t\t\t\t},\n"+
			"\t\t\t\t},\n"+
			"\t\t\t},",
		g, k, v)
}

// isRootType returns true if the function body contains "kind", "apiVersion"
// and "metadata" as property keys — the hallmark of a top-level k8s resource.
func isRootType(funcBody string) bool {
	return strings.Contains(funcBody, `"kind"`) &&
		strings.Contains(funcBody, `"apiVersion"`) &&
		strings.Contains(funcBody, `"metadata"`)
}

func main() {
	flag.Parse()
	if *filePath == "" || *group == "" || *prefix == "" {
		fmt.Fprintln(os.Stderr, "usage: fix-openapi-gvk --file <path> --group <group> --prefix <prefix>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "  --prefix  underscore-delimited package path from function names")
		fmt.Fprintln(os.Stderr, "            e.g. config_server_apis_config_v1alpha1")
		fmt.Fprintln(os.Stderr, "            the API version (v1alpha1) is derived automatically")
		os.Exit(1)
	}

	version, err := extractVersion(*prefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("derived API version: %s\n", version)

	src, err := os.ReadFile(*filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading %s: %v\n", *filePath, err)
		os.Exit(1)
	}

	// Match schema function definitions.
	funcRe := regexp.MustCompile(`(?m)^func (schema_[a-zA-Z0-9_]+)\(ref `)

	schemaOpenRe := regexp.MustCompile(`^\t\tSchema:\s+spec\.Schema\{`)

	// Build the expected function name prefix: "schema_" + user-supplied prefix + "_"
	funcPrefix := "schema_" + *prefix + "_"

	lines := strings.Split(string(src), "\n")
	out := make([]string, 0, len(lines)+50)
	injected := 0
	skippedList := 0
	skippedPrefix := 0

	i := 0
	for i < len(lines) {
		line := lines[i]
		m := funcRe.FindStringSubmatch(line)
		if m == nil {
			out = append(out, line)
			i++
			continue
		}

		funcName := m[1]

		// Kind = last underscore-delimited segment
		parts := strings.Split(funcName, "_")
		kind := parts[len(parts)-1]

		// Collect entire function body
		funcLines := []string{line}
		depth := 0
		i++
		for i < len(lines) {
			fl := lines[i]
			funcLines = append(funcLines, fl)
			for _, ch := range fl {
				switch ch {
				case '{':
					depth++
				case '}':
					depth--
				}
			}
			i++
			if depth == 0 {
				break
			}
		}

		funcBody := strings.Join(funcLines, "\n")

		// --- Filter: only process functions matching the prefix ---
		if !strings.HasPrefix(funcName, funcPrefix) {
			out = append(out, funcLines...)
			skippedPrefix++
			continue
		}

		// --- Filter: skip List types ---
		if strings.HasSuffix(kind, "List") {
			out = append(out, funcLines...)
			skippedList++
			fmt.Printf("skipped List type: %s\n", kind)
			continue
		}

		// Already patched?
		if strings.Contains(funcBody, "x-kubernetes-group-version-kind") {
			out = append(out, funcLines...)
			continue
		}

		// Only inject for root types (have kind/apiVersion/metadata properties)
		if !isRootType(funcBody) {
			out = append(out, funcLines...)
			continue
		}

		// Find the Schema: spec.Schema{ line, then find its matching closing },
		// and insert VendorExtensible just before that closing line.
		schemaLineIdx := -1
		for j, fl := range funcLines {
			if schemaOpenRe.MatchString(fl) {
				schemaLineIdx = j
				break
			}
		}

		if schemaLineIdx < 0 {
			fmt.Fprintf(os.Stderr, "WARNING: no Schema: spec.Schema{ found in %s, skipping\n", kind)
			out = append(out, funcLines...)
			continue
		}

		// Track brace depth from the Schema line to find its closing },
		schemaDepth := 0
		schemaCloseIdx := -1
		for j := schemaLineIdx; j < len(funcLines); j++ {
			fl := funcLines[j]
			for _, ch := range fl {
				if ch == '{' {
					schemaDepth++
				} else if ch == '}' {
					schemaDepth--
				}
			}
			if schemaDepth == 0 {
				schemaCloseIdx = j
				break
			}
		}

		if schemaCloseIdx < 0 {
			fmt.Fprintf(os.Stderr, "WARNING: could not find closing of Schema{} in %s, skipping\n", kind)
			out = append(out, funcLines...)
			continue
		}

		// Insert VendorExtensible before the closing line of Schema{}
		var patchedLines []string
		for j, fl := range funcLines {
			if j == schemaCloseIdx {
				patchedLines = append(patchedLines, vendorBlock(*group, kind, version))
			}
			patchedLines = append(patchedLines, fl)
		}

		out = append(out, patchedLines...)
		injected++
		fmt.Printf("injected GVK extension: %s\n", kind)
	}

	if err := os.WriteFile(*filePath, []byte(strings.Join(out, "\n")), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing %s: %v\n", *filePath, err)
		os.Exit(1)
	}

	fmt.Printf("done: injected %d, skipped %d List types, skipped %d non-matching prefix\n", injected, skippedList, skippedPrefix)
}
