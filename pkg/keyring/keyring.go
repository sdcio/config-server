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

// Package keyring holds the AES keys used to encrypt and decrypt SensitiveConfig
// and TargetSnapshot payloads.
//
// The keyring is sourced from a projected Secret volume rather than read through
// the Kubernetes API. That keeps `secrets` out of each binary's RBAC, turns a
// missing keyring into a scheduling failure rather than a crashloop, and lets
// both the controller and the apiserver load it identically with no dependency
// on manager start ordering.
//
// Rotation is driven by FileWatcher (watcher.go), which reloads the keyring in
// place when kubelet republishes the volume.
package keyring

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"k8s.io/apimachinery/pkg/util/validation"
)

// DataKey is the file name inside the mounted keyring directory. It is also the
// Secret data key the volume projects from, so the two always agree.
const DataKey = "keyring.json"

// LabelKeyID is the label written onto encrypted objects recording which key ID
// their payload was encrypted with. It makes "is anything still using key X?"
// a plain label selector, which is the gate on safely retiring a key:
//
//	kubectl get sensitiveconfigs -A -l config.sdcio.dev/keyid=key-1
//
// Key IDs are therefore constrained to valid label values — enforced at parse
// time so a bad ID is rejected on load rather than discovered on write.
const LabelKeyID = "config.sdcio.dev/keyid"

// ErrUnknownKeyID is returned by Decrypt when the payload references a key ID
// this keyring does not hold.
//
// Callers should treat this as RETRYABLE, not terminal. During rotation each
// replica picks up the new keyring independently (kubelet propagation is per
// node and takes up to ~60s), so a payload encrypted by a replica that is ahead
// is briefly undecryptable by one that is behind. Requeue with backoff; a
// terminal failure here turns a self-healing window into a stuck object.
//
// It is only permanent if a key was retired while payloads still referenced it.
var ErrUnknownKeyID = errors.New("unknown key ID")

// keyRingData is the JSON structure stored in the keyring file / Secret.
//
//	{
//	  "primary": "v2",
//	  "keys": {
//	    "v1": "base64-encoded-32-bytes...",
//	    "v2": "base64-encoded-32-bytes..."
//	  }
//	}
type keyRingData struct {
	Primary string            `json:"primary"`
	Keys    map[string]string `json:"keys"` // keyID → base64-encoded AES key
}

// KeyRing holds the set of AES keys used to encrypt and decrypt SensitiveConfig
// and TargetSnapshot payloads. It is loaded at startup and reloaded in place on
// rotation. All methods are safe for concurrent use.
type KeyRing struct {
	mu      sync.RWMutex
	primary string
	keys    map[string][]byte // keyID → raw AES key

	// path is the backing file, set only by NewFromFile. Empty for keyrings
	// built from bytes (tests), which makes Refresh a no-op there.
	path string

	// lastRefresh throttles on-demand Refresh. Guarded by mu.
	lastRefresh time.Time
}

// NewFromFile loads a keyring from a file — the default, used with a projected
// Secret volume.
func NewFromFile(path string) (*KeyRing, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read keyring %q: %w", path, err)
	}
	kr := &KeyRing{path: path}
	if err := kr.LoadBytes(raw); err != nil {
		return nil, fmt.Errorf("load keyring %q: %w", path, err)
	}
	return kr, nil
}

// NewFromBytes loads a keyring from raw JSON. Useful for tests and for callers
// that obtain the bytes some other way — e.g. a Secret:
//
//	kr, err := keyring.NewFromBytes(secret.Data[keyring.DataKey])
//
// The package deliberately does not import corev1: extracting the data key is
// the caller's business, and keeping it out means the keyring has no Kubernetes
// dependency at all.
func NewFromBytes(raw []byte) (*KeyRing, error) {
	kr := &KeyRing{}
	return kr, kr.LoadBytes(raw)
}

// LoadBytes parses raw JSON and atomically installs it as the active key set.
//
// Parsing happens outside the lock, so a malformed payload can never leave the
// keyring half-updated: on error the previous key set stays in place untouched.
func (kr *KeyRing) LoadBytes(raw []byte) error {
	primary, keys, err := parseKeyRing(raw)
	if err != nil {
		return err
	}

	kr.mu.Lock()
	defer kr.mu.Unlock()
	kr.primary, kr.keys = primary, keys
	return nil
}

// refreshThrottle bounds how often an on-demand Refresh actually touches disk.
const refreshThrottle = time.Second

// Refresh re-reads the backing file on demand. Intended for the ErrUnknownKeyID
// path: a miss means this replica may simply be behind the current keyring, and
// re-reading is cheaper than waiting for the next poll tick.
func (kr *KeyRing) Refresh() error {
	kr.mu.Lock()
	if kr.path == "" || time.Since(kr.lastRefresh) < refreshThrottle {
		kr.mu.Unlock()
		return nil
	}
	kr.lastRefresh = time.Now()
	path := kr.path
	kr.mu.Unlock()

	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("refresh keyring %q: %w", path, err)
	}
	return kr.LoadBytes(raw) // takes the lock itself
}

// parseKeyRing validates and decodes the keyring JSON. Pure: no receiver, no
// lock, no side effects.
func parseKeyRing(raw []byte) (string, map[string][]byte, error) {
	var data keyRingData
	if err := json.Unmarshal(raw, &data); err != nil {
		return "", nil, fmt.Errorf("unmarshal keyring: %w", err)
	}
	if data.Primary == "" {
		return "", nil, errors.New("keyring has no primary key")
	}
	if len(data.Keys) == 0 {
		return "", nil, errors.New("keyring has no keys")
	}

	keys := make(map[string][]byte, len(data.Keys))
	for id, b64 := range data.Keys {
		if errs := validation.IsValidLabelValue(id); len(errs) > 0 {
			return "", nil, fmt.Errorf(
				"keyID %q is not a valid label value (%s) — key IDs are recorded on objects as the %s label",
				id, strings.Join(errs, "; "), LabelKeyID,
			)
		}
		key, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return "", nil, fmt.Errorf("keyID %q: base64 decode: %w", id, err)
		}
		if l := len(key); l != 16 && l != 24 && l != 32 {
			return "", nil, fmt.Errorf("keyID %q: key must be 16, 24, or 32 bytes, got %d", id, l)
		}
		keys[id] = key
	}

	if _, ok := keys[data.Primary]; !ok {
		return "", nil, fmt.Errorf("primary keyID %q not found in keys", data.Primary)
	}
	return data.Primary, keys, nil
}

// PrimaryID returns the current primary key ID.
func (kr *KeyRing) PrimaryID() string {
	kr.mu.RLock()
	defer kr.mu.RUnlock()
	return kr.primary
}

// KeyIDs returns all key IDs currently held, sorted. Used for logging and for
// the rotation drain metric.
func (kr *KeyRing) KeyIDs() []string {
	kr.mu.RLock()
	ids := make([]string, 0, len(kr.keys))
	for id := range kr.keys {
		ids = append(ids, id)
	}
	kr.mu.RUnlock()

	sort.Strings(ids)
	return ids
}

// Has reports whether the keyring holds the given key ID. Lets callers
// distinguish "not yet propagated" from a genuine decrypt failure without
// attempting the decrypt.
func (kr *KeyRing) Has(keyID string) bool {
	kr.mu.RLock()
	defer kr.mu.RUnlock()
	_, ok := kr.keys[keyID]
	return ok
}

// NeedsReencryption reports whether the payload was encrypted with a key that is
// no longer primary — i.e. it should be re-encrypted. Safe to call without
// decrypting.
func (kr *KeyRing) NeedsReencryption(payload configv1alpha1.EncryptedPayload) bool {
	if payload.KeyID == "" {
		// Empty KeyID = old payload with data: null. Force re-resolution so the
		// Resolver writes a proper encrypted payload.
		return true
	}
	kr.mu.RLock()
	defer kr.mu.RUnlock()
	return payload.KeyID != kr.primary
}

// Encrypt encrypts plaintext with the current primary key using AES-GCM and
// returns an EncryptedPayload with KeyID and Data populated.
//
// PlainHash is NOT set here — the caller sets it from the hash of the plaintext,
// so the hash is always of the original data.
func (kr *KeyRing) Encrypt(plaintext []byte) (configv1alpha1.EncryptedPayload, error) {
	kr.mu.RLock()
	keyID := kr.primary
	key, ok := kr.keys[keyID]
	kr.mu.RUnlock()

	// Invariant: parseKeyRing guarantees primary ∈ keys. Checked anyway so a
	// future refactor that breaks it fails with a readable error rather than
	// "invalid key size 0".
	if !ok {
		return configv1alpha1.EncryptedPayload{},
			fmt.Errorf("primary keyID %q missing from keyring", keyID)
	}

	// aad is nil today. See aesGCMEncrypt for why binding ciphertext to the
	// owning object is worth doing before this format carries real data.
	ct, err := aesGCMEncrypt(key, plaintext, nil)
	if err != nil {
		return configv1alpha1.EncryptedPayload{}, fmt.Errorf("encrypt: %w", err)
	}

	return configv1alpha1.EncryptedPayload{
		KeyID: keyID,
		Data:  ct,
		// PlainHash set by caller
	}, nil
}

// Decrypt decrypts the payload using the key identified by payload.KeyID.
// Returns an error wrapping ErrUnknownKeyID if that key is not held — see
// ErrUnknownKeyID for why callers should requeue rather than fail.
func (kr *KeyRing) Decrypt(payload configv1alpha1.EncryptedPayload) ([]byte, error) {
	if payload.KeyID == "" || len(payload.Data) == 0 {
		return nil, errors.New("payload has no key ID or data")
	}

	kr.mu.RLock()
	key, ok := kr.keys[payload.KeyID]
	primary := kr.primary
	kr.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf(
			"%w %q (primary is %q): either this replica has not picked up the rotated keyring yet, or the key was retired while payloads still referenced it",
			ErrUnknownKeyID, payload.KeyID, primary,
		)
	}

	plain, err := aesGCMDecrypt(key, payload.Data, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt keyID %q: %w", payload.KeyID, err)
	}
	return plain, nil
}

// ── AES-GCM primitives ───────────────────────────────────────────────────────

// aesGCMEncrypt encrypts plaintext with AES-GCM.
// Output format: [ nonce (12 bytes) | ciphertext+tag ]
//
// aad is threaded through but nil at every call site today. It should become
// the identity of the owning object (namespace/name/path): without it the
// ciphertext is bound to nothing, so anyone who can create a SensitiveConfig can
// paste another object's Data blob into theirs and have it decrypt cleanly.
// Adding it is a format change, so it needs a version discriminator on
// EncryptedPayload — cheap now, effectively impossible once the field holds
// real data. The re-encryption machinery built for key rotation is the same
// machinery that would migrate v1→v2.
func aesGCMEncrypt(key, plaintext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	// Seal appends ciphertext+tag to nonce.
	return gcm.Seal(nonce, nonce, plaintext, aad), nil
}

// aesGCMDecrypt decrypts AES-GCM ciphertext produced by aesGCMEncrypt.
func aesGCMDecrypt(key, ciphertext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short (%d bytes)", len(ciphertext))
	}

	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plain, err := gcm.Open(nil, nonce, ct, aad)
	if err != nil {
		return nil, fmt.Errorf("authentication failed (wrong key or corrupted data): %w", err)
	}
	return plain, nil
}
