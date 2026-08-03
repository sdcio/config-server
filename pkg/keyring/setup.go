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

package keyring

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/henderiw/logger/log"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// DefaultPath is the projected Secret volume location. The DIRECTORY is the
// mount point — see FileWatcher for why subPath breaks rotation.
const DefaultPath = "/etc/sdc/keyring/" + DataKey

// PathEnvVar overrides DefaultPath.
const PathEnvVar = "SDC_KEYRING_PATH"

// Path returns the keyring file path. Defined here rather than in each main so
// the two binaries cannot drift.
func Path() string {
	if p, found := os.LookupEnv(PathEnvVar); found {
		return p
	}
	return DefaultPath
}

// Load loads the keyring from the mounted volume and registers the rotation
// watcher on mgr. Both binaries call this, so neither can drift from the other
// in how it loads, watches, or reports the keyring.
//
// The returned channel is how rotation actually reaches consumers. mgr.Add
// starts FileWatcher when mgr.Start runs; on a primary change the watcher calls
// LoadBytes on the SAME *KeyRing the reconcilers hold — so PrimaryID() simply
// starts returning the new value, with no re-injection anywhere — and then
// pushes onto this channel. Without that push the keyring would update but
// nothing would re-encrypt.
//
// Returns (nil, nil, nil) when no keyring is mounted. Absence is not an error
// here, because whether it matters depends on which reconcilers are enabled:
// consumers call ControllerConfig.RequireKeyRing during SetupWithManager, which
// fails startup with a message naming the reconciler that wanted it.
//
// A keyring that is present but unreadable or malformed IS an error. Treating
// that the same as "not mounted" would turn a typo in the Secret into a silent
// no-op for every sensitive-data path — the failure mode most worth avoiding.
func Load(ctx context.Context, mgr manager.Manager) (*KeyRing, chan event.GenericEvent, error) {
	log := log.FromContext(ctx)
	path := Path()

	kr, err := NewFromFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		log.Info("no keyring mounted, sensitive data reconcilers will not start", "path", path)
		return nil, nil, nil
	case err != nil:
		return nil, nil, err
	}

	log.Info("keyring loaded", "path", path, "primary", kr.PrimaryID(), "keys", kr.KeyIDs())
	RecordInitial(kr)

	// Buffered depth 1: a rotation is never lost, and a sweep that has not run
	// yet absorbs any further rotations that land before it does.
	rotation := make(chan event.GenericEvent, 1)

	// Interval left zero — FileWatcher falls back to defaultPollInterval.
	if err := mgr.Add(&FileWatcher{
		Path:    path,
		KeyRing: kr,
		OnChange: func(oldPrimary, newPrimary string) {
			select {
			case rotation <- event.GenericEvent{
				// Non-nil placeholder: the consumer's map function ignores the
				// object and lists everything needing re-encryption.
				Object: &configv1alpha1.SensitiveConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "keyring-rotation"},
				},
			}:
			default:
				log.Info("keyring rotation sweep already pending",
					"from", oldPrimary, "to", newPrimary)
			}
		},
	}); err != nil {
		return nil, nil, fmt.Errorf("add keyring watcher: %w", err)
	}

	return kr, rotation, nil
}