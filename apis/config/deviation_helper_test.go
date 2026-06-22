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

package config

import "testing"

func TestDeviationName(t *testing.T) {
	cases := map[string]struct {
		typ      DeviationType
		resource string
		want     string
	}{
		"config": {DeviationType_CONFIG, "intent1-srl-srl1", "config-intent1-srl-srl1"},
		"target": {DeviationType_TARGET, "srl1", "target-srl1"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := DeviationName(tc.typ, tc.resource); got != tc.want {
				t.Fatalf("DeviationName(%q, %q) = %q, want %q", tc.typ, tc.resource, got, tc.want)
			}
		})
	}
}

func TestParseDeviationName(t *testing.T) {
	cases := map[string]struct {
		input        string
		wantType     DeviationType
		wantResource string
		wantOK       bool
	}{
		"config prefixed": {"config-intent1-srl-srl1", DeviationType_CONFIG, "intent1-srl-srl1", true},
		"target prefixed": {"target-srl1", DeviationType_TARGET, "srl1", true},
		// legacy CRs predate the prefix; ok=false so callers keep the raw name
		"no known prefix": {"intent1-srl-srl1", "", "", false},
		// strip only the leading prefix even when the resource itself begins with one
		"resource name starts with prefix literal": {"config-config-name", DeviationType_CONFIG, "config-name", true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			typ, resource, ok := ParseDeviationName(tc.input)
			if ok != tc.wantOK || typ != tc.wantType || resource != tc.wantResource {
				t.Fatalf("ParseDeviationName(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.input, typ, resource, ok, tc.wantType, tc.wantResource, tc.wantOK)
			}
		})
	}
}
