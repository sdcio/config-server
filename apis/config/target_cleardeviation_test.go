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

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/types"
)

func TestResolveClearDeviationConfig(t *testing.T) {
	cfg := &Config{}
	cfg.Name = "intent1-srl-srl1"

	configs := map[string]*Config{cfg.Name: cfg}
	targetKey := types.NamespacedName{Namespace: "default", Name: "srl1"}

	cases := map[string]struct {
		input        string
		wantCfg      *Config
		wantErrMatch string
	}{
		"exact match by config name":     {"intent1-srl-srl1", cfg, ""},
		"match via deviation prefix":     {"config-intent1-srl-srl1", cfg, ""},
		"unknown name":                   {"missing", nil, "not found"},
		"unknown after prefix strip":     {"config-missing", nil, "not found"},
		"target-typed deviation rejects": {"target-srl1", nil, "target-scoped"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := resolveClearDeviationConfig(tc.input, configs, targetKey)
			if tc.wantErrMatch != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErrMatch) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErrMatch, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantCfg {
				t.Fatalf("got %v, want %v", got, tc.wantCfg)
			}
		})
	}
}
