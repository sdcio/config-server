/*
Copyright 2023 The xxx Authors.

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
	"fmt"
	"testing"
)

func TestValidate(t *testing.T) {
	cases := map[string]struct {
		path        string
		expectedErr error
	}{
		"NoDiscovery": {
			path:        "../../../example/discovery-rule/nodiscovery.yaml",
			expectedErr: nil,
		},
		"DiscoveryPrefix": {
			path:        "../../../example/discovery-rule/discovery_prefix.yaml",
			expectedErr: nil,
		},
		"DiscoveryAddress": {
			path:        "../../../example/discovery-rule/discovery_prefix.yaml",
			expectedErr: nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dr, err := GetDiscoveryRuleFromFile(tc.path)
			if err != nil {
				t.Errorf("unexpected error\n%s", err.Error())
			}
			fmt.Println(name, dr)
			err = dr.Validate()
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
