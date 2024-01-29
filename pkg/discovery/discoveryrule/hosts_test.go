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
	"fmt"
	"testing"

	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
)

func TestGetHosts(t *testing.T) {
	cases := map[string]struct {
		DRPrefixes []invv1alpha1.DiscoveryRulePrefix
	}{
		"Normal": {
			DRPrefixes: []invv1alpha1.DiscoveryRulePrefix{
				{Prefix: "10.0.0.0/29"},
			},
		},
		"Exclude": {
			DRPrefixes: []invv1alpha1.DiscoveryRulePrefix{
				{Prefix: "10.0.0.0/29", Excludes: []string{"10.0.0.2", "10.0.0.5"}},
				{Prefix: "10.0.1.0/29", Excludes: []string{"10.1.0.2", "10.0.1.5"}},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			hosts, _ := getHosts(tc.DRPrefixes)
			fmt.Println(hosts)
		})
	}
}
