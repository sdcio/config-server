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
	"crypto/sha256"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/henderiw/logger/log"
)

const (
	// defaultPollInterval bounds how long a rotation can go unnoticed if inotify
	// events are dropped or unavailable.
	defaultPollInterval = 30 * time.Second

	// debounceDelay coalesces the burst of events kubelet produces when it
	// republishes a Secret volume.
	debounceDelay = 100 * time.Millisecond
)

// FileWatcher reloads a KeyRing in place when its backing file changes.
//
// Kubernetes projects Secret volumes through a symlink indirection:
//
//	keyring.json -> ..data/keyring.json
//	..data       -> ..2026_08_03_10_00_00.123456789/
//
// On update kubelet writes a new timestamped directory and rename(2)s it over
// ..data, so updates are atomic — a partial file is never observed. Two
// consequences drive this implementation:
//
//   - The DIRECTORY is watched, never the file. fsnotify resolves symlinks and
//     watches the target inode; when kubelet swaps ..data the old inode is
//     unlinked, and a file watch dies silently and permanently after one REMOVE.
//   - The volume must NOT be mounted with subPath. subPath is bind-mounted to
//     the inode present at container start and never updates, which would make
//     rotation silently stop working.
//
// Propagation is bounded by kubelet's pod sync loop — up to ~60s, independently
// per node. Replicas therefore disagree about the primary key for a short window
// after promotion; see ErrUnknownKeyID.
type FileWatcher struct {
	// Path is the keyring file inside the mounted directory.
	Path string

	// KeyRing is reloaded in place. Callers keep their existing pointer.
	KeyRing *KeyRing

	// Interval is the poll fallback. Zero means defaultPollInterval.
	Interval time.Duration

	// OnChange fires only when the PRIMARY key ID changes — not on every reload.
	// Adding a key to the ring is not a rotation; promoting one is.
	//
	// Called from the watcher goroutine, so it must not block. Reloading the
	// keyring does not re-encrypt anything by itself: this hook is what wakes
	// the Resolver so payloads start draining off the old key.
	OnChange func(oldPrimary, newPrimary string)

	lastHash [32]byte
}

// NeedLeaderElection returns false: every replica holds its own keyring and must
// reload independently.
func (w *FileWatcher) NeedLeaderElection() bool { return false }

// Start runs until ctx is cancelled. Implements manager.Runnable.
func (w *FileWatcher) Start(ctx context.Context) error {
	log := log.FromContext(ctx).With("keyring", w.Path)

	interval := w.Interval
	if interval <= 0 {
		interval = defaultPollInterval
	}

	// Seed lastHash from what is already on disk. OnChange is guarded on a
	// primary change, so this initial pass is a no-op for a keyring that main
	// already loaded.
	w.reloadIfChanged(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var events chan fsnotify.Event
	var errs chan error

	// Degrade to polling rather than failing the manager if the watch cannot be
	// established. Some CSI-backed volumes emit no inotify events at all;
	// rotation still converges within one interval.
	if fsw, err := newDirWatcher(w.Path); err != nil {
		log.Error("cannot watch keyring directory, falling back to polling",
			"interval", interval, "err", err)
	} else {
		defer func() { _ = fsw.Close() }()
		events, errs = fsw.Events, fsw.Errors
	}

	// time.After leaks a timer per burst until it fires; at ~a dozen events per
	// kubelet republish that is not worth the Reset/drain subtlety.
	var debounce <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-events:
			debounce = time.After(debounceDelay)

		case err := <-errs:
			// Watch errors do not stop the loop; the ticker still covers us.
			log.Error("keyring watch error", "err", err)

		case <-debounce:
			debounce = nil
			w.reloadIfChanged(ctx)

		case <-ticker.C:
			w.reloadIfChanged(ctx)
		}
	}
}

// newDirWatcher watches the directory containing path. Watching the directory
// rather than the file is what survives kubelet's ..data symlink swap.
func newDirWatcher(path string) (*fsnotify.Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := fsw.Add(filepath.Dir(path)); err != nil {
		_ = fsw.Close()
		return nil, err
	}
	return fsw, nil
}

// reloadIfChanged re-reads the file and installs it only if the bytes differ.
//
// On any failure the previous keyring is kept and lastHash is deliberately NOT
// updated, so the next tick retries. A typo'd keyring degrades to "rotation has
// not happened yet" rather than killing the process — which is the right
// failure mode, but only if it is loud. Hence the metric and the error log:
// otherwise rotation appears to hang with no visible cause.
func (w *FileWatcher) reloadIfChanged(ctx context.Context) {
	log := log.FromContext(ctx).With("keyring", w.Path)

	raw, err := os.ReadFile(w.Path)
	if err != nil {
		recordReload(resultReadError)
		log.Error("cannot read keyring, keeping previous keyring", "err", err)
		return
	}

	h := sha256.Sum256(raw)
	if h == w.lastHash {
		return
	}

	oldPrimary := w.KeyRing.PrimaryID()
	if err := w.KeyRing.LoadBytes(raw); err != nil {
		recordReload(resultParseError)
		log.Error("cannot parse keyring, keeping previous keyring",
			"primary", oldPrimary, "err", err)
		return
	}

	w.lastHash = h
	newPrimary := w.KeyRing.PrimaryID()
	recordReload(resultOK)
	recordKeyRing(w.KeyRing)

	if newPrimary == oldPrimary {
		log.Info("keyring reloaded", "primary", newPrimary, "keys", w.KeyRing.KeyIDs())
		return
	}

	log.Info("keyring primary rotated",
		"from", oldPrimary, "to", newPrimary, "keys", w.KeyRing.KeyIDs())
	if w.OnChange != nil {
		w.OnChange(oldPrimary, newPrimary)
	}
}