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
	"fmt"

	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/sdcio/config-server/pkg/keyring"
	targetmanager "github.com/sdcio/config-server/pkg/sdc/target/manager"
	"github.com/sdcio/sdc-protos/config_read"
)

// toLastAppliedConfigEntry maps a TargetSnapshot.Spec.Configs entry — the
// last-applied resolved state for one intent, keyed by intent name — onto
// the wire ConfigEntry shape. It is the last-applied counterpart to
// toConfigEntry: same output type and field mapping, but reading a single
// SensitiveConfigSpec value straight off a TargetSnapshot instead of joining
// a live Config + SensitiveConfig.
//
// A decrypt or unmarshal failure returns an error rather than being
// skipped: unlike recovery.go's best-effort crash-recovery posture, a read
// RPC feeding data-server's diff must not silently omit an intent's
// last-applied content.
func toLastAppliedConfigEntry(name string, spec configv1alpha1.SensitiveConfigSpec, kr *keyring.KeyRing) (*config_read.ConfigEntry, error) {
	plain, err := kr.Decrypt(spec.Payload)
	if err != nil {
		return nil, fmt.Errorf("decrypt last-applied config %s: %w", name, err)
	}

	var blobs []config.ConfigBlob
	if err := json.Unmarshal(plain, &blobs); err != nil {
		return nil, fmt.Errorf("unmarshal last-applied config %s: %w", name, err)
	}

	sensitivePaths, err := targetmanager.ParseSensitivePaths(spec.SensitivePaths)
	if err != nil {
		return nil, err
	}

	configBlobs := make([]*config_read.ConfigBlob, 0, len(blobs))
	for _, b := range blobs {
		configBlobs = append(configBlobs, &config_read.ConfigBlob{Path: b.Path, Value: b.Value.Raw})
	}

	return &config_read.ConfigEntry{
		Name:           name,
		NonRevertive:   spec.Revertive != nil && !*spec.Revertive,
		Orphan:         spec.Lifecycle != nil && spec.Lifecycle.DeletionPolicy == configv1alpha1.DeletionOrphan,
		Priority:       int32(spec.Priority),
		SensitivePaths: sensitivePaths,
		Config:         configBlobs,
	}, nil
}
