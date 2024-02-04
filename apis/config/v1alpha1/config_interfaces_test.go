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
	"fmt"
	"testing"
)

func TestExampleConfig(t *testing.T) {
	cases := map[string]struct {
		path        string
		expectedErr error
	}{
		"Config": {
			path:        "../../../example/config/config.yaml",
			expectedErr: nil,
		},
		"ConfigBadTargetRef": {
			path: "../../../example/config/bad_targetref_config.yaml",
			expectedErr: fmt.Errorf("%s\n%s",
				"a config cr always need a config.sdcio.dev/targetName",
				"a config cr always need a config.sdcio.dev/targetNamespace",
			),
		},
		"ConfigBadSpec": {
			path: "../../../example/config/bad_spec_config.yaml",
			expectedErr: fmt.Errorf("json: cannot unmarshal string into Go struct field ConfigSpec.spec.priority of type int"),
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := GetConfigFromFile(tc.path)
			if err != nil {
				if tc.expectedErr == nil {
					t.Errorf("unexpected error\n%s", err.Error())
				}
				return
			}
			err = cfg.Validate()
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
