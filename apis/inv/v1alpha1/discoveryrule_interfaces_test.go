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
	"testing"

	"k8s.io/utils/pointer"
)

func TestValidate(t *testing.T) {
	cases := map[string]struct {
		dr          *DiscoveryRule
		expectedErr error
	}{
		"Static": {
			dr: &DiscoveryRule{
				Spec: DiscoveryRuleSpec{
					Addresses: []DiscoveryRuleAddress{
						{Address: "1.1.1.1", HostName: "dev1"},
						{Address: "1.1.1.2", HostName: "dev2"},
					},
					DiscoveryParameters: DiscoveryParameters{
						DefaultSchema: &SchemaKey{
							Provider: "x.y.z",
							Version:  "v1",
						},
						TargetConnectionProfiles: []TargetProfile{
							{
								Credentials:       "x",
								ConnectionProfile: "a",
								SyncProfile:       pointer.String("b"),
							},
						},
					},
				},
			},
			expectedErr: nil,
		},
		"Dynamic": {
			dr: &DiscoveryRule{
				Spec: DiscoveryRuleSpec{
					Prefixes: []DiscoveryRulePrefix{
						{Prefix: "1.1.1.0/24"},
					},
					DiscoveryParameters: DiscoveryParameters{
						DiscoveryProfile: &DiscoveryProfile{
							Credentials:        "x",
							ConnectionProfiles: []string{"a"},
						},
						TargetConnectionProfiles: []TargetProfile{
							{
								Credentials:       "x",
								ConnectionProfile: "a",
								SyncProfile:       pointer.String("b"),
							},
						},
					},
				},
			},
			expectedErr: nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.dr.Validate()

			if err != nil {
				if tc.expectedErr == nil {
					t.Errorf("%sunexpected error\n%s", name, err.Error())
				}
				return
			}
			if tc.expectedErr != nil {
				t.Errorf("%s expecting an error, got nil", name)
			}
		})
	}
}
