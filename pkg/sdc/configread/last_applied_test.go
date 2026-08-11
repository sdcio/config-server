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

package configread

import (
	"encoding/json"
	"testing"

	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/sdcio/config-server/pkg/keyring"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

// mkSnapshotEntry encrypts blobs into a SensitiveConfigSpec.Payload the same
// way configresolver's reconciler does, so tests exercise the real
// encrypt/decrypt round trip instead of a hand-built payload.
func mkSnapshotEntry(t *testing.T, kr *keyring.KeyRing, blobs []config.ConfigBlob, mutate func(*configv1alpha1.SensitiveConfigSpec)) configv1alpha1.SensitiveConfigSpec {
	t.Helper()
	data, err := json.Marshal(blobs)
	if err != nil {
		t.Fatalf("marshal blobs: %v", err)
	}
	payload, err := kr.Encrypt(data)
	if err != nil {
		t.Fatalf("encrypt blobs: %v", err)
	}
	spec := configv1alpha1.SensitiveConfigSpec{
		Priority: 10,
		Payload:  payload,
	}
	if mutate != nil {
		mutate(&spec)
	}
	return spec
}

func TestToLastAppliedConfigEntry_happyPath(t *testing.T) {
	kr := newTestKeyRing(t)
	blobs := []config.ConfigBlob{
		{Path: "/system", Value: runtime.RawExtension{Raw: []byte(`{"hostname":"router1"}`)}},
	}
	spec := mkSnapshotEntry(t, kr, blobs, func(s *configv1alpha1.SensitiveConfigSpec) {
		s.SensitivePaths = []string{"/interface/name"}
	})

	entry, err := toLastAppliedConfigEntry("cfg1", spec, kr)
	if err != nil {
		t.Fatalf("toLastAppliedConfigEntry: %v", err)
	}
	if entry.GetName() != "cfg1" {
		t.Errorf("name = %q, want cfg1", entry.GetName())
	}
	if entry.GetPriority() != 10 {
		t.Errorf("priority = %d, want 10", entry.GetPriority())
	}
	if len(entry.GetConfig()) != 1 || entry.GetConfig()[0].GetPath() != "/system" {
		t.Fatalf("config blobs = %+v", entry.GetConfig())
	}
	if len(entry.GetSensitivePaths()) != 1 {
		t.Fatalf("sensitive_paths = %+v, want 1 entry", entry.GetSensitivePaths())
	}
}

// TestToLastAppliedConfigEntry_fieldMapping locks orphan/revertive/priority
// mapping straight off SensitiveConfigSpec, independent of any live Config.
func TestToLastAppliedConfigEntry_fieldMapping(t *testing.T) {
	kr := newTestKeyRing(t)
	spec := mkSnapshotEntry(t, kr, nil, func(s *configv1alpha1.SensitiveConfigSpec) {
		s.Revertive = ptr.To(false)
		s.Lifecycle = &configv1alpha1.Lifecycle{DeletionPolicy: configv1alpha1.DeletionOrphan}
		s.Priority = 42
	})

	entry, err := toLastAppliedConfigEntry("cfg1", spec, kr)
	if err != nil {
		t.Fatalf("toLastAppliedConfigEntry: %v", err)
	}
	if !entry.GetNonRevertive() {
		t.Errorf("non_revertive = false, want true (Revertive=false)")
	}
	if !entry.GetOrphan() {
		t.Errorf("orphan = false, want true (DeletionOrphan)")
	}
	if entry.GetPriority() != 42 {
		t.Errorf("priority = %d, want 42", entry.GetPriority())
	}
}

func TestToLastAppliedConfigEntry_defaultsRevertiveTrue(t *testing.T) {
	kr := newTestKeyRing(t)
	spec := mkSnapshotEntry(t, kr, nil, nil) // Revertive unset

	entry, err := toLastAppliedConfigEntry("cfg1", spec, kr)
	if err != nil {
		t.Fatalf("toLastAppliedConfigEntry: %v", err)
	}
	if entry.GetNonRevertive() {
		t.Errorf("non_revertive = true, want false (Revertive defaults to true)")
	}
}

// TestToLastAppliedConfigEntry_decryptFailure locks the "fail the whole
// call" contract: a bad Payload (e.g. an unknown KeyID, as after a key was
// retired before rotation completed) must return an error, not be silently
// skipped — a silently-missing entry is exactly the deletion-loss failure
// mode this mapping exists to prevent.
func TestToLastAppliedConfigEntry_decryptFailure(t *testing.T) {
	kr := newTestKeyRing(t)
	spec := configv1alpha1.SensitiveConfigSpec{
		Payload: configv1alpha1.EncryptedPayload{
			KeyID: "unknown-key",
			Data:  []byte("not-real-ciphertext"),
		},
	}

	_, err := toLastAppliedConfigEntry("cfg1", spec, kr)
	if err == nil {
		t.Fatal("toLastAppliedConfigEntry with unknown KeyID: want error, got nil")
	}
}

// TestToLastAppliedConfigEntry_unmarshalFailure locks the same
// fail-the-whole-call contract for a payload that decrypts fine but whose
// plaintext isn't valid []ConfigBlob JSON.
func TestToLastAppliedConfigEntry_unmarshalFailure(t *testing.T) {
	kr := newTestKeyRing(t)
	payload, err := kr.Encrypt([]byte("not valid json"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	spec := configv1alpha1.SensitiveConfigSpec{Payload: payload}

	_, err = toLastAppliedConfigEntry("cfg1", spec, kr)
	if err == nil {
		t.Fatal("toLastAppliedConfigEntry with invalid blob JSON: want error, got nil")
	}
}
