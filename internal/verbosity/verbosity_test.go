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

package verbosity

import (
	"log/slog"
	"testing"
)

func TestFromArgs(t *testing.T) {
	cases := map[string]struct {
		args     []string
		expected int
	}{
		"no args":          {args: nil, expected: 0},
		"long":             {args: []string{"--v=4"}, expected: 4},
		"short":            {args: []string{"-v=4"}, expected: 4},
		"separate value":   {args: []string{"-v", "4"}, expected: 4},
		"unparsable value": {args: []string{"--v=loud"}, expected: 0},
		"different flag":   {args: []string{"--vmodule=reconciler=4"}, expected: 0},
		"apiserver flags only": {args: []string{
			"--tls-cert-file", "/apiserver.local.config/certificates/tls.crt",
			"--audit-log-path=-",
			"--secure-port=6443",
		}, expected: 0},
		"among apiserver flags": {args: []string{
			"--tls-cert-file", "/apiserver.local.config/certificates/tls.crt",
			"--audit-log-path=-",
			"--v=4",
			"--secure-port=6443",
		}, expected: 4},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := FromArgs(tc.args); got != tc.expected {
				t.Errorf("FromArgs(%q) = %v, want %v", tc.args, got, tc.expected)
			}
		})
	}
}

func TestLogLevel(t *testing.T) {
	cases := map[int]slog.Level{
		0: slog.LevelInfo,
		4: slog.LevelDebug,
	}

	for verbosity, expected := range cases {
		if got := LogLevel(verbosity); got != expected {
			t.Errorf("LogLevel(%d) = %v, want %v", verbosity, got, expected)
		}
	}
}
