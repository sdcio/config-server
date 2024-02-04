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
)

func TestExampleConfigSet(t *testing.T) {
	cases := map[string]struct {
		path        string
		expectedErr error
	}{
		"Config": {
			path:        "../../../example/config/configset.yaml",
			expectedErr: nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := GetConfigSetFromFile(tc.path)
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
