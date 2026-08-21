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

package targetmanager

import (
	"testing"

	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
)

func TestConfigDeviationKey(t *testing.T) {
	cases := map[string]struct {
		in       string
		wantNS   string
		wantName string
		wantOK   bool
	}{
		"gvk nsn": {
			in:       "default.srl2-config",
			wantNS:   "default",
			wantName: configv1alpha1.DeviationName(configv1alpha1.DeviationType_CONFIG, "srl2-config"),
			wantOK:   true,
		},
		"name itself contains a dot": {
			in:       "prod.rack1.srl1-cfg",
			wantNS:   "prod",
			wantName: configv1alpha1.DeviationName(configv1alpha1.DeviationType_CONFIG, "rack1.srl1-cfg"),
			wantOK:   true,
		},
		// This is the panic that took down the controller: SplitN on a
		// bare Config name (no namespace) produced a 1-element slice and
		// processDeviations indexed parts[1] before checking len.
		"bare config name does not panic": {
			in:     "srl2-config",
			wantOK: false,
		},
		"empty name after dot": {
			in:     "default.",
			wantOK: false,
		},
		"empty namespace": {
			in:     ".srl2-config",
			wantOK: false,
		},
		"empty": {
			in:     "",
			wantOK: false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := configDeviationKey(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("configDeviationKey(%q) ok=%v, want %v (nsn=%+v)", tc.in, ok, tc.wantOK, got)
			}
			if !tc.wantOK {
				return
			}
			if got.Namespace != tc.wantNS || got.Name != tc.wantName {
				t.Fatalf("configDeviationKey(%q) = {%s/%s}, want {%s/%s}",
					tc.in, got.Namespace, got.Name, tc.wantNS, tc.wantName)
			}
		})
	}
}
