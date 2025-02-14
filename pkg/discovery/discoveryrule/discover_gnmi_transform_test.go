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

package discoveryrule

import (
	"regexp"
	"testing"
)

// Test ApplyRegex function
func TestApplyRegex(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		value       string
		expected    string
		shouldError bool
	}{
		{
			name:     "Simple match",
			pattern:  `v(\d+\.\d+\.\d+)-.*`,
			value:    "v1.2.3-release",
			expected: "1.2.3",
		},
		{
			name:     "Extract major version",
			pattern:  `(\d+)\.\d+\.\d+`,
			value:    "10.5.3",
			expected: "10",
		},
		{
			name:     "Extract serial number",
			pattern:  `SN:(\d{4}-\d{4})`,
			value:    "Device SN:1234-5678 active",
			expected: "1234-5678",
		},
		{
			name:        "No match, return original",
			pattern:     `SN:(\d{4}-\d{4})`,
			value:       "No serial here",
			expected:    "No serial here",
			shouldError: false,
		},
		{
			name:        "Invalid regex pattern",
			pattern:     `[unclosed(`,
			value:       "sample",
			shouldError: true,
		},
		{
			name: "Version",
			pattern: `^v?(\d+\.\d+\.\d+)`,
			value: "v24.3.2-118-g706b4f0d99",
			expected: "24.3.2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ApplyRegex(tc.pattern, tc.value)
			if tc.shouldError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if result != tc.expected {
				t.Errorf("Expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}

// Test RunStarlark function
func TestRunStarlark(t *testing.T) {
	tests := []struct {
		name        string
		script      string
		input       string
		expected    string
		shouldError bool
	}{
		{
			name: "Simple transformation",
			script: `
def transform(value):
    return value.upper()
`,
			input:    "hello",
			expected: "HELLO",
		},
		{
			name: "Replace dots with dashes",
			script: `
def transform(value):
    return value.replace(".", "-")
`,
			input:    "1.2.3",
			expected: "1-2-3",
		},
		{
			name: "Append suffix",
			script: `
def transform(value):
    return value + "-transformed"
`,
			input:    "data",
			expected: "data-transformed",
		},
		{
			name: "Invalid script syntax",
			script: `
def transform(value)
    return value + "!"
`, // Missing `:` in function definition
			input:       "error",
			shouldError: true,
		},
		{
			name: "Invalid return type",
			script: `
def transform(value):
    return 123  # Not a string
`,
			input:       "test",
			shouldError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := RunStarlark("test", tc.script, tc.input)
			if tc.shouldError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if result != tc.expected {
				t.Errorf("Expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}

// Test invalid regex patterns separately
func TestApplyRegex_InvalidRegex(t *testing.T) {
	_, err := regexp.Compile("[invalid(regex")
	if err == nil {
		t.Fatal("Expected regex compilation error but got none")
	}
}
