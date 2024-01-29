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

func TestGetVendorType(t *testing.T) {
	cases := map[string]struct {
		provider     string
		outputVendor string
		outputType   string
	}{
		"Empty": {
			provider:     "",
			outputVendor: "",
			outputType:   "",
		},
		"One": {
			provider:     "x",
			outputVendor: "",
			outputType:   "",
		},
		"Two": {
			provider:     "x.y",
			outputVendor: "x",
			outputType:   "y",
		},
		"Three": {
			provider:     "x.y.z",
			outputVendor: "x",
			outputType:   "y",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			outputVendor, outputType := GetVendorType(tc.provider)

			if diff := cmp.Diff(outputVendor, tc.outputVendor); diff != "" {
				t.Errorf("TestGetVendorType: vendor -want, +got:\n%s", diff)
			}
			if diff := cmp.Diff(outputType, tc.outputType); diff != "" {
				t.Errorf("TestGetVendorType: type -want, +got:\n%s", diff)
			}
		})
	}
}
