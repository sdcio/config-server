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

// Package verbosity resolves the log level the binaries are started with.
package verbosity

import (
	"flag"
	"io"
	"log/slog"

	"github.com/spf13/pflag"
)

const (
	flagName  = "v"
	flagUsage = "number for the log level verbosity"
)

// RegisterFlag declares -v unless one of the logging libraries already did.
func RegisterFlag() {
	if flag.Lookup(flagName) == nil {
		flag.Int(flagName, 0, flagUsage)
	}
}

// FromArgs returns the verbosity requested through -v, ignoring every other
// flag. The arguments are parsed here because the logger is built before the
// flag sets that own them are.
func FromArgs(args []string) int {
	fs := pflag.NewFlagSet(flagName, pflag.ContinueOnError)
	fs.ParseErrorsAllowlist = pflag.ParseErrorsAllowlist{UnknownFlags: true}
	fs.SetOutput(io.Discard)
	verbosity := fs.IntP(flagName, flagName, 0, flagUsage)
	if err := fs.Parse(args); err != nil {
		return 0
	}
	return *verbosity
}

// LogLevel follows the logr mapping of V(0) onto info and V(4) onto debug.
func LogLevel(verbosity int) slog.Level {
	return slog.Level(-verbosity)
}
