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

package targetmanager

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"

	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/sdcio/data-server/pkg/api"
)

// ComputeCreateDSHash returns a stable hex digest of the datastore spec and the
// resource versions of the objects it was resolved from. Equal inputs hash
// equal and any change alters the digest, so the reconciler recreates the
// datastore when it differs.
//
// The hash is over the resolved api.DatastoreSpec, so it changes exactly when
// the datastore the data server would build changes. TestHashChangesOnEveryField
// fails when a spec field is added without being hashed here. Changing the
// encoding changes every digest and recreates all datastores.
func ComputeCreateDSHash(spec api.DatastoreSpec, refs *configv1alpha1.TargetStatusUsedReferences) string {
	w := hashWriter{h: sha256.New()}
	w.str(spec.Name)
	w.target(&spec.Target)
	w.str(spec.Schema.Vendor)
	w.str(spec.Schema.Version)
	w.i64(int64(len(spec.Sync.Configs)))
	for i := range spec.Sync.Configs {
		c := &spec.Sync.Configs[i]
		w.str(c.Name)
		if w.present(c.Target != nil) {
			w.target(c.Target)
		}
		w.i64(int64(len(c.Paths)))
		for _, p := range c.Paths {
			w.str(p)
		}
		w.i64(int64(c.Mode))
		w.i64(int64(c.Interval))
	}
	if w.present(refs != nil) {
		w.str(refs.ConnectionProfileResourceVersion)
		w.str(refs.SyncProfileResourceVersion)
		w.str(refs.SecretResourceVersion)
		w.str(refs.TLSSecretResourceVersion)
	}
	return hex.EncodeToString(w.h.Sum(nil))
}

// hashWriter writes length-prefixed fields so adjacent values cannot collide.
type hashWriter struct {
	h hash.Hash
}

func (w hashWriter) i64(v int64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(v))
	w.h.Write(b[:])
}

func (w hashWriter) str(s string) {
	w.i64(int64(len(s)))
	w.h.Write([]byte(s))
}

func (w hashWriter) bool(v bool) {
	if v {
		w.h.Write([]byte{1})
		return
	}
	w.h.Write([]byte{0})
}

// present writes a presence marker and returns ok, so nil and zero values hash apart.
func (w hashWriter) present(ok bool) bool {
	w.bool(ok)
	return ok
}

func (w hashWriter) target(t *api.Target) {
	w.str(string(t.Type))
	w.str(t.Address)
	w.i64(int64(t.Port))
	if w.present(t.Credentials != nil) {
		w.str(t.Credentials.Username)
		w.str(t.Credentials.Password)
		w.str(t.Credentials.Token)
	}
	if w.present(t.TLS != nil) {
		w.str(t.TLS.CA)
		w.str(t.TLS.Cert)
		w.str(t.TLS.Key)
		w.bool(t.TLS.SkipVerify)
	}
	if g := t.ProtocolOptions.GNMI; w.present(g != nil) {
		w.str(string(g.Encoding))
		w.str(g.TargetName)
	}
	if n := t.ProtocolOptions.Netconf; w.present(n != nil) {
		w.bool(n.IncludeNs)
		w.bool(n.OperationWithNs)
		w.bool(n.UseOperationRemove)
		w.i64(int64(n.CommitCandidate))
	}
}
