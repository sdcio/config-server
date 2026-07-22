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

package configresolver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/sdcio/config-server/pkg/keyring"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// ── test helpers ────────────────────────────────────────────────────────────────

func blob(path, jsonValue string) configv1alpha1.ConfigBlob {
	return configv1alpha1.ConfigBlob{
		Path:  path,
		Value: runtime.RawExtension{Raw: []byte(jsonValue)},
	}
}

// mustJSONEqual compares two JSON byte slices structurally (key order agnostic).
func mustJSONEqual(t *testing.T, got []byte, wantJSON string) {
	t.Helper()
	var g, w interface{}
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("unmarshal got %q: %v", got, err)
	}
	if err := json.Unmarshal([]byte(wantJSON), &w); err != nil {
		t.Fatalf("unmarshal want %q: %v", wantJSON, err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("json mismatch:\n got: %#v\nwant: %#v", g, w)
	}
}

// ── exactVarName / isValidVarName ────────────────────────────────────────────────

// Test_exactVarName locks the exact-match contract: only a whole-value, single,
// well-formed ${vars.<name>} is a reference. Everything else (interpolation,
// multiple placeholders, missing delimiters, bad charset) returns "" and is
// therefore routed to resolveLeaf's marker guard rather than substituted.
func Test_exactVarName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"${vars.pw}", "pw"},
		{"${vars.my.var-name_1}", "my.var-name_1"}, // full CRD charset: . - _
		{"${vars.}", ""},                           // empty name
		{"${vars.a}${vars.b}", ""},                 // two placeholders -> invalid middle
		{"${vars.pw", ""},                          // no closing brace
		{"vars.pw}", ""},                           // no opening marker
		{"foo${vars.pw}", ""},                      // marker not at start (not whole value)
		{"${vars.pw}bar", ""},                      // brace not at end (not whole value)
		{"${vars.a b}", ""},                        // space not in charset
		{"${vars.a/b}", ""},                        // slash not in charset
		{"plain", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := exactVarName(c.in); got != c.want {
			t.Errorf("exactVarName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func Test_isValidVarName(t *testing.T) {
	valid := []string{"abc", "a.b-c_1", "A0", "x.y.z", "kebab-case", "snake_case"}
	for _, s := range valid {
		if !isValidVarName(s) {
			t.Errorf("isValidVarName(%q) = false, want true", s)
		}
	}
	invalid := []string{"a}", "a$", "a b", "a/b", "a:b", "a[0]"}
	for _, s := range invalid {
		if isValidVarName(s) {
			t.Errorf("isValidVarName(%q) = true, want false", s)
		}
	}
}

// ── resolveLeaf (the engine seam) ────────────────────────────────────────────────

func Test_resolveLeaf(t *testing.T) {
	lr := &leafResolver{
		values:     map[string]string{"pw": "S3CR3T"},
		unresolved: map[string]error{"broken": errors.New("secret \"nope\" not found")},
	}

	t.Run("exact hit substitutes and is sensitive", func(t *testing.T) {
		got, sensitive, err := lr.resolveLeaf("${vars.pw}")
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got != "S3CR3T" || !sensitive {
			t.Errorf("got (%q,%v), want (S3CR3T,true)", got, sensitive)
		}
	})

	t.Run("plain literal passes through, not sensitive", func(t *testing.T) {
		for _, s := range []string{"plain", "", "192.0.2.1", "hello world"} {
			got, sensitive, err := lr.resolveLeaf(s)
			if err != nil || got != s || sensitive {
				t.Errorf("resolveLeaf(%q) = (%q,%v,%v), want (%q,false,nil)", s, got, sensitive, err, s)
			}
		}
	})

	t.Run("undefined var errors", func(t *testing.T) {
		_, _, err := lr.resolveLeaf("${vars.ghost}")
		if err == nil || !strings.Contains(err.Error(), "undefined") {
			t.Errorf("want undefined error, got %v", err)
		}
	})

	t.Run("unresolved referenced var surfaces its reason", func(t *testing.T) {
		_, _, err := lr.resolveLeaf("${vars.broken}")
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Errorf("want fetch-reason error, got %v", err)
		}
	})

	t.Run("interpolation / malformed are rejected, never shipped", func(t *testing.T) {
		// Includes a value that *contains* a resolvable var — still rejected,
		// because exact-match mode does not interpolate.
		for _, s := range []string{
			"foo ${vars.pw} bar", // interpolation attempt
			"${vars.pw}${vars.pw}", // two placeholders
			"${vars.pw",            // missing closing brace
			"prefix ${vars.",       // bare marker mid-string
		} {
			got, sensitive, err := lr.resolveLeaf(s)
			if err == nil {
				t.Errorf("resolveLeaf(%q) = (%q,%v,nil), want error", s, got, sensitive)
			}
		}
	})
}

// ── substituteBlobs (the single walk) ────────────────────────────────────────────

func Test_substituteBlobs(t *testing.T) {
	lr := &leafResolver{values: map[string]string{"pw": "s3cr3t", "cert": "CERTDATA"}}

	cases := []struct {
		name      string
		blobs     []configv1alpha1.ConfigBlob
		wantVals  []string // expected resolved JSON per blob, in order
		wantPaths []string
	}{
		{
			name:      "map value",
			blobs:     []configv1alpha1.ConfigBlob{blob("/system", `{"auth":{"password":"${vars.pw}"}}`)},
			wantVals:  []string{`{"auth":{"password":"s3cr3t"}}`},
			wantPaths: []string{"/system/auth/password"},
		},
		{
			// leaf-list: two refs at the SAME keyless path collapse to one path.
			name:      "leaf-list array elements",
			blobs:     []configv1alpha1.ConfigBlob{blob("/", `{"acls":["${vars.pw}","plain","${vars.cert}"]}`)},
			wantVals:  []string{`{"acls":["s3cr3t","plain","CERTDATA"]}`},
			wantPaths: []string{"/acls"},
		},
		{
			// keyed list: each entry resolved independently, single keyless path.
			name:      "keyed list, secret per entry",
			blobs:     []configv1alpha1.ConfigBlob{blob("/", `{"interface":[{"name":"eth0","hash":"${vars.pw}"},{"name":"eth1","hash":"${vars.cert}"}]}`)},
			wantVals:  []string{`{"interface":[{"name":"eth0","hash":"s3cr3t"},{"name":"eth1","hash":"CERTDATA"}]}`},
			wantPaths: []string{"/interface/hash"},
		},
		{
			name:      "deeply nested map and array",
			blobs:     []configv1alpha1.ConfigBlob{blob("/", `{"a":{"b":[{"c":"${vars.pw}"}]}}`)},
			wantVals:  []string{`{"a":{"b":[{"c":"s3cr3t"}]}}`},
			wantPaths: []string{"/a/b/c"},
		},
		{
			name:      "blob path key predicate stripped to prefix",
			blobs:     []configv1alpha1.ConfigBlob{blob("/network-instance[name=default]", `{"protocols":{"bgp":{"auth":"${vars.pw}"}}}`)},
			wantVals:  []string{`{"protocols":{"bgp":{"auth":"s3cr3t"}}}`},
			wantPaths: []string{"/network-instance/protocols/bgp/auth"},
		},
		{
			// Root-level bare string: substituted, but path "" is not recorded.
			name:      "root bare string substituted, no path captured",
			blobs:     []configv1alpha1.ConfigBlob{blob("/", `"${vars.pw}"`)},
			wantVals:  []string{`"s3cr3t"`},
			wantPaths: nil,
		},
		{
			name:      "non-ref scalars untouched",
			blobs:     []configv1alpha1.ConfigBlob{blob("/", `{"x":"hello","y":42,"z":true,"n":null}`)},
			wantVals:  []string{`{"x":"hello","y":42,"z":true,"n":null}`},
			wantPaths: nil,
		},
		{
			name: "multiple blobs: paths deduped and sorted",
			blobs: []configv1alpha1.ConfigBlob{
				blob("/z", `{"k":"${vars.pw}"}`),
				blob("/a", `{"k":"${vars.cert}"}`),
			},
			wantVals:  []string{`{"k":"s3cr3t"}`, `{"k":"CERTDATA"}`},
			wantPaths: []string{"/a/k", "/z/k"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, paths, err := lr.substituteBlobs(c.blobs)
			if err != nil {
				t.Fatalf("substituteBlobs: %v", err)
			}
			if len(out) != len(c.wantVals) {
				t.Fatalf("got %d blobs, want %d", len(out), len(c.wantVals))
			}
			for i := range out {
				if out[i].Path != c.blobs[i].Path {
					t.Errorf("blob[%d] path changed: %q -> %q", i, c.blobs[i].Path, out[i].Path)
				}
				mustJSONEqual(t, out[i].Value.Raw, c.wantVals[i])
			}
			if !reflect.DeepEqual(paths, c.wantPaths) {
				t.Errorf("paths: got %#v, want %#v", paths, c.wantPaths)
			}
		})
	}
}

func Test_substituteBlobs_errors(t *testing.T) {
	lr := &leafResolver{
		values:     map[string]string{"pw": "s3cr3t"},
		unresolved: map[string]error{"broken": errors.New("secret \"x\" not found")},
	}
	cases := []struct {
		name    string
		blobs   []configv1alpha1.ConfigBlob
		wantSub string // substring expected in the error
	}{
		{
			name:    "interpolation attempt (even with a resolvable var)",
			blobs:   []configv1alpha1.ConfigBlob{blob("/", `{"x":"scheme://${vars.pw}@host"}`)},
			wantSub: "exact",
		},
		{
			name:    "undefined variable",
			blobs:   []configv1alpha1.ConfigBlob{blob("/", `{"x":"${vars.ghost}"}`)},
			wantSub: "undefined",
		},
		{
			name:    "referenced but unresolved variable",
			blobs:   []configv1alpha1.ConfigBlob{blob("/", `{"x":"${vars.broken}"}`)},
			wantSub: "not found",
		},
		{
			name:    "error is path-qualified",
			blobs:   []configv1alpha1.ConfigBlob{blob("/sys", `{"auth":{"pw":"${vars.ghost}"}}`)},
			wantSub: "/sys/auth/pw",
		},
		{
			name:    "malformed json",
			blobs:   []configv1alpha1.ConfigBlob{blob("/", `{not json`)},
			wantSub: "unmarshal",
		},
		{
			name:    "malformed blob path",
			blobs:   []configv1alpha1.ConfigBlob{blob("/foo[bar", `{"x":"${vars.pw}"}`)},
			wantSub: "path",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := lr.substituteBlobs(c.blobs)
			if err == nil {
				t.Fatalf("expected error")
			}
			if c.wantSub != "" && !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error %q does not contain %q", err.Error(), c.wantSub)
			}
		})
	}
}

// Test_substituteBlobs_unusedBrokenVarPasses asserts that an unresolved variable
// which is NOT referenced in the blobs does not fail the walk — the failure
// decision is made at point of use, so a declared-but-unused var whose secret is
// missing is tolerated.
func Test_substituteBlobs_unusedBrokenVarPasses(t *testing.T) {
	lr := &leafResolver{
		values:     map[string]string{"used": "OK"},
		unresolved: map[string]error{"unused": errors.New("secret missing")},
	}
	blobs := []configv1alpha1.ConfigBlob{blob("/", `{"a":"${vars.used}","b":"plain"}`)}
	out, paths, err := lr.substituteBlobs(blobs)
	if err != nil {
		t.Fatalf("unexpected error from unused broken var: %v", err)
	}
	mustJSONEqual(t, out[0].Value.Raw, `{"a":"OK","b":"plain"}`)
	if !reflect.DeepEqual(paths, []string{"/a"}) {
		t.Errorf("paths: got %#v, want [/a]", paths)
	}
}

// ── keylessXPath ────────────────────────────────────────────────────────────────

func Test_keylessXPath(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"/", "", false},
		{"/interfaces/hash", "/interfaces/hash", false},
		{"/network-instance[name=default]/protocols", "/network-instance/protocols", false},
		{"/a[k1=v1][k2=v2]/b[k=v]/c", "/a/b/c", false},
		{"/foo[bar", "", true}, // unbalanced key predicate -> ParsePath errors
	}
	for _, c := range cases {
		got, err := keylessXPath(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("%q: expected error, got %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q: got %q, want %q", c.in, got, c.want)
		}
	}
}

// ── test fixtures for reconciler-backed tests ────────────────────────────────────

const testNS = "default"

// newTestKeyRing builds a real *keyring.KeyRing from an in-memory Secret. The
// actual key bytes are irrelevant to detectChange — only the primary ID matters
// for NeedsReencryption — but they must be a valid AES key length so that the
// resolveConfig round-trip (Encrypt/Decrypt) works.
func newTestKeyRing(t *testing.T, primary string, keyIDs ...string) *keyring.KeyRing {
	t.Helper()
	keys := make(map[string]string, len(keyIDs))
	for _, id := range keyIDs {
		k := make([]byte, 32)
		for i := range k {
			k[i] = byte(i)
		}
		keys[id] = base64.StdEncoding.EncodeToString(k)
	}
	raw, err := json.Marshal(map[string]interface{}{"primary": primary, "keys": keys})
	if err != nil {
		t.Fatalf("marshal keyring: %v", err)
	}
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNS, Name: "keyring"},
		Data:       map[string][]byte{"keyring.json": raw},
	}
	kr, err := keyring.NewFromSecret(sec)
	if err != nil {
		t.Fatalf("NewFromSecret: %v", err)
	}
	return kr
}

func mkSecret(name string, data map[string]string) *corev1.Secret {
	d := make(map[string][]byte, len(data))
	for k, v := range data {
		d[k] = []byte(v)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNS, Name: name},
		Data:       d,
	}
}

func mkVar(name, secretName, key string) configv1alpha1.ConfigVar {
	return configv1alpha1.ConfigVar{
		Name: name,
		SecretRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
			Key:                  key,
		},
	}
}

func mkConfig(vars []configv1alpha1.ConfigVar, blobs ...configv1alpha1.ConfigBlob) *configv1alpha1.Config {
	return &configv1alpha1.Config{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNS, Name: "cfg1"},
		Spec:       configv1alpha1.ConfigSpec{Config: blobs, Vars: vars},
	}
}

func newTestReconciler(t *testing.T, kr *keyring.KeyRing, objs ...client.Object) *reconciler {
	t.Helper()
	sch := runtime.NewScheme()
	if err := corev1.AddToScheme(sch); err != nil {
		t.Fatalf("add corev1 to scheme: %v", err)
	}
	c := fake.NewClientBuilder().WithScheme(sch).WithObjects(objs...).Build()
	return &reconciler{keyring: kr, apiReader: c}
}

// ── buildLeafResolver ────────────────────────────────────────────────────────────

func Test_buildLeafResolver(t *testing.T) {
	ctx := context.Background()

	t.Run("nil fetched map is initialized", func(t *testing.T) {
		r := newTestReconciler(t, nil, mkSecret("mysecret", map[string]string{"key0": "val0"}))
		cfg := mkConfig([]configv1alpha1.ConfigVar{mkVar("pw", "mysecret", "key0")})

		lr, _, _, err := r.buildLeafResolver(ctx, cfg, nil)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if lr.values["pw"] != "val0" {
			t.Errorf("values[pw] = %q, want val0", lr.values["pw"])
		}
	})

	t.Run("resolved var: value, hash and secret name recorded", func(t *testing.T) {
		r := newTestReconciler(t, nil, mkSecret("mysecret", map[string]string{"key0": "val0"}))
		cfg := mkConfig([]configv1alpha1.ConfigVar{mkVar("pw", "mysecret", "key0")})

		lr, hashes, names, err := r.buildLeafResolver(ctx, cfg, map[string]*corev1.Secret{})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if lr.values["pw"] != "val0" {
			t.Errorf("values[pw] = %q, want val0", lr.values["pw"])
		}
		if len(lr.unresolved) != 0 {
			t.Errorf("unresolved should be empty, got %v", lr.unresolved)
		}
		if hashes["mysecret/key0"] != sha256hex([]byte("val0")) {
			t.Errorf("hash mismatch: %v", hashes)
		}
		if !reflect.DeepEqual(names, []string{"mysecret"}) {
			t.Errorf("secretNames = %#v, want [mysecret]", names)
		}
	})

	t.Run("missing secret is tolerated (recorded, not fatal)", func(t *testing.T) {
		r := newTestReconciler(t, nil) // no secrets in cluster
		cfg := mkConfig([]configv1alpha1.ConfigVar{mkVar("pw", "nosuch", "key0")})

		lr, hashes, names, err := r.buildLeafResolver(ctx, cfg, map[string]*corev1.Secret{})
		if err != nil {
			t.Fatalf("missing secret must not be fatal, got: %v", err)
		}
		if _, ok := lr.unresolved["pw"]; !ok {
			t.Errorf("expected pw recorded as unresolved")
		}
		if _, ok := lr.values["pw"]; ok {
			t.Errorf("pw must not be in values")
		}
		if len(hashes) != 0 {
			t.Errorf("no hashes expected, got %v", hashes)
		}
		// Secret name still labelled so a later create re-triggers the watch.
		if !reflect.DeepEqual(names, []string{"nosuch"}) {
			t.Errorf("secretNames = %#v, want [nosuch]", names)
		}
	})

	t.Run("present secret, missing key is tolerated", func(t *testing.T) {
		r := newTestReconciler(t, nil, mkSecret("mysecret", map[string]string{"other": "x"}))
		cfg := mkConfig([]configv1alpha1.ConfigVar{mkVar("pw", "mysecret", "key0")})

		lr, _, _, err := r.buildLeafResolver(ctx, cfg, map[string]*corev1.Secret{})
		if err != nil {
			t.Fatalf("missing key must not be fatal, got: %v", err)
		}
		if _, ok := lr.unresolved["pw"]; !ok {
			t.Errorf("expected pw recorded as unresolved (missing key)")
		}
	})

	t.Run("nil secretRef is recorded, not added to secret names", func(t *testing.T) {
		r := newTestReconciler(t, nil)
		cfg := mkConfig([]configv1alpha1.ConfigVar{{Name: "pw"}}) // SecretRef nil

		lr, _, names, err := r.buildLeafResolver(ctx, cfg, map[string]*corev1.Secret{})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if _, ok := lr.unresolved["pw"]; !ok {
			t.Errorf("expected pw recorded as unresolved (no secretRef)")
		}
		if len(names) != 0 {
			t.Errorf("nil secretRef must not contribute a secret name, got %#v", names)
		}
	})

	t.Run("two vars share a secret: name deduped, both keys hashed", func(t *testing.T) {
		r := newTestReconciler(t, nil, mkSecret("s", map[string]string{"k1": "v1", "k2": "v2"}))
		cfg := mkConfig([]configv1alpha1.ConfigVar{mkVar("a", "s", "k1"), mkVar("b", "s", "k2")})

		lr, hashes, names, err := r.buildLeafResolver(ctx, cfg, map[string]*corev1.Secret{})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if lr.values["a"] != "v1" || lr.values["b"] != "v2" {
			t.Errorf("values = %#v", lr.values)
		}
		if len(hashes) != 2 {
			t.Errorf("want 2 hashes, got %#v", hashes)
		}
		if !reflect.DeepEqual(names, []string{"s"}) {
			t.Errorf("secretNames = %#v, want [s] (deduped)", names)
		}
	})
}

// ── resolveConfig (end-to-end fetch + walk + encrypt) ────────────────────────────

func Test_resolveConfig(t *testing.T) {
	ctx := context.Background()
	kr := newTestKeyRing(t, "v1", "v1")

	t.Run("referenced var: substituted, encrypted, path + hash captured", func(t *testing.T) {
		r := newTestReconciler(t, kr, mkSecret("mysecret", map[string]string{"key0": "topsecret"}))
		cfg := mkConfig(
			[]configv1alpha1.ConfigVar{mkVar("pw", "mysecret", "key0")},
			blob("/system", `{"auth":{"admin":{"password":"${vars.pw}"}}}`),
		)

		out, err := r.resolveConfig(ctx, cfg, map[string]*corev1.Secret{})
		if err != nil {
			t.Fatalf("resolveConfig: %v", err)
		}
		if !reflect.DeepEqual(out.sensitivePaths, []string{"/system/auth/admin/password"}) {
			t.Errorf("sensitivePaths = %#v", out.sensitivePaths)
		}
		if out.secretKeyHashes["mysecret/key0"] != sha256hex([]byte("topsecret")) {
			t.Errorf("secretKeyHashes = %#v", out.secretKeyHashes)
		}
		if !reflect.DeepEqual(out.secretNames, []string{"mysecret"}) {
			t.Errorf("secretNames = %#v", out.secretNames)
		}

		// Decrypt and confirm the placeholder is gone and the value substituted.
		plain, err := kr.Decrypt(out.payload)
		if err != nil {
			t.Fatalf("decrypt: %v", err)
		}
		if out.payload.PlainHash != sha256hex(plain) {
			t.Errorf("PlainHash does not match decrypted payload")
		}
		var blobs []configv1alpha1.ConfigBlob
		if err := json.Unmarshal(plain, &blobs); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if strings.Contains(string(blobs[0].Value.Raw), "${vars.") {
			t.Errorf("placeholder survived into encrypted payload: %s", blobs[0].Value.Raw)
		}
		mustJSONEqual(t, blobs[0].Value.Raw, `{"auth":{"admin":{"password":"topsecret"}}}`)
	})

	t.Run("no variables: payload self-contained, no paths or hashes", func(t *testing.T) {
		r := newTestReconciler(t, kr)
		cfg := mkConfig(nil, blob("/system", `{"hostname":"router1"}`))

		out, err := r.resolveConfig(ctx, cfg, map[string]*corev1.Secret{})
		if err != nil {
			t.Fatalf("resolveConfig: %v", err)
		}
		if len(out.sensitivePaths) != 0 {
			t.Errorf("want no paths, got %#v", out.sensitivePaths)
		}
		if len(out.secretKeyHashes) != 0 {
			t.Errorf("want no hashes, got %#v", out.secretKeyHashes)
		}
		plain, err := kr.Decrypt(out.payload)
		if err != nil {
			t.Fatalf("decrypt: %v", err)
		}
		var blobs []configv1alpha1.ConfigBlob
		if err := json.Unmarshal(plain, &blobs); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		mustJSONEqual(t, blobs[0].Value.Raw, `{"hostname":"router1"}`)
	})

	t.Run("undefined referenced var fails resolution", func(t *testing.T) {
		r := newTestReconciler(t, kr)
		cfg := mkConfig(nil, blob("/", `{"x":"${vars.ghost}"}`))

		if _, err := r.resolveConfig(ctx, cfg, map[string]*corev1.Secret{}); err == nil {
			t.Fatal("expected error for undefined variable")
		}
	})

	t.Run("unused broken var does not block an otherwise-clean config", func(t *testing.T) {
		r := newTestReconciler(t, kr) // 'nosuch' secret absent
		cfg := mkConfig(
			[]configv1alpha1.ConfigVar{mkVar("unused", "nosuch", "k")},
			blob("/system", `{"hostname":"router1"}`),
		)

		out, err := r.resolveConfig(ctx, cfg, map[string]*corev1.Secret{})
		if err != nil {
			t.Fatalf("unused broken var must not block resolution: %v", err)
		}
		if len(out.sensitivePaths) != 0 {
			t.Errorf("want no paths, got %#v", out.sensitivePaths)
		}
	})
}

// ── detectChange ────────────────────────────────────────────────────────────────

func Test_detectChange(t *testing.T) {
	// Keyed lists (not leaf-lists), single and multiple entries — now in the
	// ${vars.<name>} model with a declared Vars block.
	singleEntry := mkConfig(
		[]configv1alpha1.ConfigVar{mkVar("key0", "mysecret", "key0")},
		blob("/", `{"interface":[{"name":"eth0","auth":{"hash":"${vars.key0}"}}]}`),
	)
	multiEntry := mkConfig(
		[]configv1alpha1.ConfigVar{mkVar("key0", "mysecret", "key0"), mkVar("key1", "mysecret", "key1")},
		blob("/", `{"interface":[
			{"name":"eth0","auth":{"hash":"${vars.key0}"}},
			{"name":"eth1","auth":{"hash":"${vars.key1}"}}
		]}`),
	)

	hashOf := func(c *configv1alpha1.Config) string {
		h, err := c.Spec.GetHash()
		if err != nil {
			t.Fatalf("GetHash: %v", err)
		}
		return h
	}

	// Real keyring, primary "v1". Payload KeyID "v1" is current; anything else
	// (or empty) reads as needing re-encryption.
	kr := newTestKeyRing(t, "v1", "v1")

	cases := []struct {
		name string
		cfg  *configv1alpha1.Config

		hasExisting     bool
		existingHash    string            // ConfigHash stored on the existing SC
		storedKeyHashes map[string]string // SecretKeyHashes stored on the existing SC
		payloadKeyID    string            // KeyID stored on the existing payload

		secrets []client.Object // Secrets present in the cluster

		want changeResult
	}{
		{
			name:        "no existing SC -> configChanged",
			cfg:         singleEntry,
			hasExisting: false,
			want:        changeResult{configChanged: true},
		},
		{
			name:            "single entry, nothing changed",
			cfg:             singleEntry,
			hasExisting:     true,
			existingHash:    hashOf(singleEntry),
			storedKeyHashes: map[string]string{"mysecret/key0": sha256hex([]byte("val0"))},
			payloadKeyID:    "v1",
			secrets:         []client.Object{mkSecret("mysecret", map[string]string{"key0": "val0"})},
			want:            changeResult{},
		},
		{
			name:            "config hash differs -> configChanged short-circuits secret check",
			cfg:             singleEntry,
			hasExisting:     true,
			existingHash:    "STALE_HASH",
			storedKeyHashes: map[string]string{"mysecret/key0": sha256hex([]byte("val0"))},
			payloadKeyID:    "v1",
			// secret value also differs, but secretChanged stays false because the
			// secret check is skipped when configChanged is already true.
			secrets: []client.Object{mkSecret("mysecret", map[string]string{"key0": "CHANGED"})},
			want:    changeResult{configChanged: true},
		},
		{
			name:            "secret value changed",
			cfg:             singleEntry,
			hasExisting:     true,
			existingHash:    hashOf(singleEntry),
			storedKeyHashes: map[string]string{"mysecret/key0": sha256hex([]byte("val0"))},
			payloadKeyID:    "v1",
			secrets:         []client.Object{mkSecret("mysecret", map[string]string{"key0": "NEWVAL"})},
			want:            changeResult{secretChanged: true},
		},
		{
			name:         "multiple entries, one secret changed",
			cfg:          multiEntry,
			hasExisting:  true,
			existingHash: hashOf(multiEntry),
			storedKeyHashes: map[string]string{
				"mysecret/key0": sha256hex([]byte("val0")),
				"mysecret/key1": sha256hex([]byte("val1")),
			},
			payloadKeyID: "v1",
			secrets: []client.Object{mkSecret("mysecret", map[string]string{
				"key0": "val0",    // unchanged
				"key1": "CHANGED", // changed
			})},
			want: changeResult{secretChanged: true},
		},
		{
			name:         "multiple entries, all secrets unchanged",
			cfg:          multiEntry,
			hasExisting:  true,
			existingHash: hashOf(multiEntry),
			storedKeyHashes: map[string]string{
				"mysecret/key0": sha256hex([]byte("val0")),
				"mysecret/key1": sha256hex([]byte("val1")),
			},
			payloadKeyID: "v1",
			secrets: []client.Object{mkSecret("mysecret", map[string]string{
				"key0": "val0",
				"key1": "val1",
			})},
			want: changeResult{},
		},
		{
			name:            "secret deleted -> secretChanged",
			cfg:             singleEntry,
			hasExisting:     true,
			existingHash:    hashOf(singleEntry),
			storedKeyHashes: map[string]string{"mysecret/key0": sha256hex([]byte("val0"))},
			payloadKeyID:    "v1",
			secrets:         nil, // secret absent from the cluster
			want:            changeResult{secretChanged: true},
		},
		{
			name:            "keyring rotated only",
			cfg:             singleEntry,
			hasExisting:     true,
			existingHash:    hashOf(singleEntry),
			storedKeyHashes: map[string]string{"mysecret/key0": sha256hex([]byte("val0"))},
			payloadKeyID:    "v0", // not the current primary
			secrets:         []client.Object{mkSecret("mysecret", map[string]string{"key0": "val0"})},
			want:            changeResult{keyringChanged: true},
		},
		{
			name:            "empty payload KeyID reads as keyring change",
			cfg:             singleEntry,
			hasExisting:     true,
			existingHash:    hashOf(singleEntry),
			storedKeyHashes: map[string]string{"mysecret/key0": sha256hex([]byte("val0"))},
			payloadKeyID:    "", // old/always-encrypt-migration payload
			secrets:         []client.Object{mkSecret("mysecret", map[string]string{"key0": "val0"})},
			want:            changeResult{keyringChanged: true},
		},
		{
			name:         "config changed AND keyring rotated",
			cfg:          multiEntry,
			hasExisting:  true,
			existingHash: "STALE_HASH",
			storedKeyHashes: map[string]string{
				"mysecret/key0": sha256hex([]byte("val0")),
				"mysecret/key1": sha256hex([]byte("val1")),
			},
			payloadKeyID: "v0",
			secrets:      []client.Object{mkSecret("mysecret", map[string]string{"key0": "val0", "key1": "val1"})},
			want:         changeResult{configChanged: true, keyringChanged: true},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestReconciler(t, kr, tt.secrets...)

			existing := &configv1alpha1.SensitiveConfig{
				Spec: configv1alpha1.SensitiveConfigSpec{
					ConfigHash:      tt.existingHash,
					SecretKeyHashes: tt.storedKeyHashes,
					Payload:         configv1alpha1.EncryptedPayload{KeyID: tt.payloadKeyID},
				},
			}

			got, _, err := r.detectChange(context.Background(), tt.cfg, existing, tt.hasExisting)
			if err != nil {
				t.Fatalf("detectChange: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}