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

package generic

import (
	"testing"

	"github.com/sdcio/config-server/pkg/registry/options"
	"github.com/stretchr/testify/assert"
)

// TestStrategy_AllowCreateOnUpdate_DefaultTrue verifies that the default
// behaviour (no options, or DisableCreateOnUpdate=false) preserves the existing
// create-on-update semantics for resources that want it.
func TestStrategy_AllowCreateOnUpdate_DefaultTrue(t *testing.T) {
	s := &strategy{}
	assert.True(t, s.AllowCreateOnUpdate(),
		"AllowCreateOnUpdate must be true when no options are set (backwards-compat default)")

	s2 := &strategy{opts: &options.Options{DisableCreateOnUpdate: false}}
	assert.True(t, s2.AllowCreateOnUpdate(),
		"AllowCreateOnUpdate must be true when DisableCreateOnUpdate=false")
}

// TestStrategy_AllowCreateOnUpdate_FalseWhenDisableCreateOnUpdateSet is the
// TDD driver for the DisableCreateOnUpdate flag.  When the registry serves
// TargetSnapshot it must return false so that a PATCH against a missing
// resource produces a clean NotFound rather than the confusing
// "update failed to construct UpdatedObject" error that the henderiw
// apiserver-store emits when it tries to merge-patch over a nil existing
// object (AllowCreateOnUpdate=true path).
func TestStrategy_AllowCreateOnUpdate_FalseWhenDisableCreateOnUpdateSet(t *testing.T) {
	s := &strategy{opts: &options.Options{DisableCreateOnUpdate: true}}
	assert.False(t, s.AllowCreateOnUpdate(),
		"AllowCreateOnUpdate must be false when DisableCreateOnUpdate=true so that "+
			"PATCH on a missing TargetSnapshot returns NotFound (not a corrupt UpdatedObject error)")
}
