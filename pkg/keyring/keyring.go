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
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/base64"
    "encoding/json"
    "fmt"
    "io"
    "sync"

    configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
    corev1 "k8s.io/api/core/v1"
)

const keyRingDataKey = "keyring.json"

// keyRingData is the JSON structure stored in the Kubernetes Secret.
//
// Example Secret data["keyring.json"]:
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

// KeyRing holds the set of AES-256 keys used to encrypt and decrypt
// SensitiveConfig and TargetSnapshot payloads.
// It is loaded once at startup from a Kubernetes Secret and reloaded
// in-place when the Secret changes (key rotation).
// All methods are safe for concurrent use.
type KeyRing struct {
    mu      sync.RWMutex
    primary string
    keys    map[string][]byte // keyID → raw 32-byte AES key
}

// NewFromSecret creates a KeyRing from a Kubernetes Secret.
// The Secret must contain a "keyring.json" key with the keyRingData structure.
func NewFromSecret(secret *corev1.Secret) (*KeyRing, error) {
    kr := &KeyRing{}
    return kr, kr.load(secret)
}

// Reload atomically replaces the key ring from an updated Secret.
// Called when the keyring Secret's metadata watch fires.
func (kr *KeyRing) Reload(secret *corev1.Secret) error {
    kr.mu.Lock()
    defer kr.mu.Unlock()
    return kr.load(secret)
}

// load parses the Secret and replaces the in-memory key set.
// Must be called with mu held (write).
func (kr *KeyRing) load(secret *corev1.Secret) error {
    raw, ok := secret.Data[keyRingDataKey]
    if !ok {
        return fmt.Errorf("secret %s/%s has no key %q",
            secret.Namespace, secret.Name, keyRingDataKey)
    }

    var data keyRingData
    if err := json.Unmarshal(raw, &data); err != nil {
        return fmt.Errorf("unmarshal keyring: %w", err)
    }
    if data.Primary == "" {
        return fmt.Errorf("keyring has no primary key")
    }
    if len(data.Keys) == 0 {
        return fmt.Errorf("keyring has no keys")
    }

    keys := make(map[string][]byte, len(data.Keys))
    for id, b64 := range data.Keys {
        key, err := base64.StdEncoding.DecodeString(b64)
        if err != nil {
            return fmt.Errorf("keyID %q: base64 decode: %w", id, err)
        }
        if l := len(key); l != 16 && l != 24 && l != 32 {
            return fmt.Errorf("keyID %q: key must be 16, 24, or 32 bytes, got %d", id, l)
        }
        keys[id] = key
    }

    if _, ok := keys[data.Primary]; !ok {
        return fmt.Errorf("primary keyID %q not found in keys", data.Primary)
    }

    kr.primary = data.Primary
    kr.keys    = keys
    return nil
}

// PrimaryID returns the current primary key ID.
func (kr *KeyRing) PrimaryID() string {
    kr.mu.RLock()
    defer kr.mu.RUnlock()
    return kr.primary
}

// NeedsReencryption returns true if the payload was encrypted with a key
// that is no longer the primary — i.e. the payload should be re-encrypted.
// Safe to call without decrypting.
func (kr *KeyRing) NeedsReencryption(payload configv1alpha1.EncryptedPayload) bool {
    if payload.KeyID == "" {
        // Empty KeyID means no encryption (no secret refs) — nothing to re-encrypt.
        return false
    }
    kr.mu.RLock()
    defer kr.mu.RUnlock()
    return payload.KeyID != kr.primary
}

// Encrypt encrypts plaintext with the current primary key using AES-GCM.
// Returns an EncryptedPayload with KeyID and Data populated.
// PlainHash is NOT set here — the caller sets it from the hash of the plaintext
// before passing it to Encrypt, so the hash is always of the original data.
func (kr *KeyRing) Encrypt(plaintext []byte) (configv1alpha1.EncryptedPayload, error) {
    kr.mu.RLock()
    key    := kr.keys[kr.primary]
    keyID  := kr.primary
    kr.mu.RUnlock()

    ct, err := aesGCMEncrypt(key, plaintext)
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
// Returns an error if the keyID is unknown (key was retired before rotation completed).
func (kr *KeyRing) Decrypt(payload configv1alpha1.EncryptedPayload) ([]byte, error) {
    if payload.KeyID == "" || len(payload.Data) == 0 {
        // No encryption — nothing to decrypt.
        return nil, nil
    }

    kr.mu.RLock()
    key, ok := kr.keys[payload.KeyID]
    kr.mu.RUnlock()

    if !ok {
        return nil, fmt.Errorf("unknown keyID %q — key removed before rotation completed", payload.KeyID)
    }

    plain, err := aesGCMDecrypt(key, payload.Data)
    if err != nil {
        return nil, fmt.Errorf("decrypt keyID %q: %w", payload.KeyID, err)
    }
    return plain, nil
}

// ── AES-GCM primitives ─────────────────────────────────────────────────────────

// aesGCMEncrypt encrypts plaintext with AES-GCM.
// Output format: [ nonce (12 bytes) | ciphertext+tag ]
func aesGCMEncrypt(key, plaintext []byte) ([]byte, error) {
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

    // Seal appends ciphertext+tag to nonce
    return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// aesGCMDecrypt decrypts AES-GCM ciphertext produced by aesGCMEncrypt.
func aesGCMDecrypt(key, ciphertext []byte) ([]byte, error) {
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
    plain, err := gcm.Open(nil, nonce, ct, nil)
    if err != nil {
        return nil, fmt.Errorf("authentication failed (wrong key or corrupted data): %w", err)
    }
    return plain, nil
}