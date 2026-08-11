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
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/sdcio/config-server/pkg/keyring"
	"github.com/sdcio/sdc-protos/config_read"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testNamespace = "ns1"
	testTarget    = "target1"
)

// ── test helpers ────────────────────────────────────────────────────────────────

func newTestServer(t *testing.T, objs ...client.Object) *Server {
	t.Helper()
	return newTestServerWithKeyRing(t, newTestKeyRing(t), objs...)
}

// newTestServerWithKeyRing is newTestServer with an explicit KeyRing, so
// tests that need to encrypt fixture payloads (e.g. via mkSnapshotEntry) can
// share the exact KeyRing instance the server under test will decrypt with.
func newTestServerWithKeyRing(t *testing.T, kr *keyring.KeyRing, objs ...client.Object) *Server {
	t.Helper()
	sch := runtime.NewScheme()
	if err := configv1alpha1.AddToScheme(sch); err != nil {
		t.Fatalf("add configv1alpha1 to scheme: %v", err)
	}
	c := fake.NewClientBuilder().WithScheme(sch).WithObjects(objs...).Build()
	s, err := NewServer(&Config{Address: "127.0.0.1:0", Client: c, KeyRing: kr})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return s
}

// newTestKeyRing builds a real *keyring.KeyRing from an in-memory Secret,
// valid for both Encrypt and Decrypt round-trips in tests.
func newTestKeyRing(t *testing.T) *keyring.KeyRing {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	raw, err := json.Marshal(map[string]interface{}{
		"primary": "v1",
		"keys":    map[string]string{"v1": base64.StdEncoding.EncodeToString(key)},
	})
	if err != nil {
		t.Fatalf("marshal keyring: %v", err)
	}
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: "keyring"},
		Data:       map[string][]byte{"keyring.json": raw},
	}
	kr, err := keyring.NewFromSecret(sec)
	if err != nil {
		t.Fatalf("NewFromSecret: %v", err)
	}
	return kr
}

// mkTargetSnapshot builds a TargetSnapshot the way targetconfig's reconciler
// saves one: named/namespaced after the target itself, keyed by intent name.
func mkTargetSnapshot(targetNS, targetName string, configs map[string]configv1alpha1.SensitiveConfigSpec) *configv1alpha1.TargetSnapshot {
	return &configv1alpha1.TargetSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: targetNS,
			Name:      targetName,
		},
		Spec: configv1alpha1.TargetSnapshotSpec{
			Configs: configs,
		},
	}
}

func mkTargetConfig(name string, targetNS, targetName string, mutate func(*configv1alpha1.Config)) *configv1alpha1.Config {
	cfg := &configv1alpha1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: targetNS,
			Name:      name,
			Labels: map[string]string{
				config.TargetNamespaceKey: targetNS,
				config.TargetNameKey:      targetName,
			},
		},
		Spec: configv1alpha1.ConfigSpec{
			Priority: 10,
			Config: []configv1alpha1.ConfigBlob{
				{Path: "/system", Value: runtime.RawExtension{Raw: []byte(`{"hostname":"router1"}`)}},
			},
		},
	}
	if mutate != nil {
		mutate(cfg)
	}
	return cfg
}

// ── Get ──────────────────────────────────────────────────────────────────────

// TestGet_found locks the last-applied contract: Get serves whatever's
// recorded in the target's TargetSnapshot, keyed by intent name — not any
// live Config.
func TestGet_found(t *testing.T) {
	kr := newTestKeyRing(t)
	blobs := []config.ConfigBlob{
		{Path: "/system", Value: runtime.RawExtension{Raw: []byte(`{"hostname":"router1"}`)}},
	}
	entrySpec := mkSnapshotEntry(t, kr, blobs, nil)
	snapshot := mkTargetSnapshot(testNamespace, testTarget, map[string]configv1alpha1.SensitiveConfigSpec{
		"cfg1": entrySpec,
	})
	s := newTestServerWithKeyRing(t, kr, snapshot)

	rsp, err := s.Get(context.Background(), &config_read.GetConfigRequest{
		TargetNamespace: testNamespace,
		TargetName:      testTarget,
		Name:            "cfg1",
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	entry := rsp.GetConfig()
	if entry.GetName() != "cfg1" || entry.GetNamespace() != testNamespace {
		t.Fatalf("unexpected entry: %+v", entry)
	}
	if entry.GetPriority() != 10 {
		t.Errorf("priority = %d, want 10", entry.GetPriority())
	}
	if len(entry.GetConfig()) != 1 || entry.GetConfig()[0].GetPath() != "/system" {
		t.Errorf("config blobs = %+v", entry.GetConfig())
	}
}

// TestGet_notFoundNoSnapshot covers a target that has never had a successful
// transaction: no TargetSnapshot exists for it at all yet.
func TestGet_notFoundNoSnapshot(t *testing.T) {
	s := newTestServer(t)

	_, err := s.Get(context.Background(), &config_read.GetConfigRequest{
		TargetNamespace: testNamespace,
		TargetName:      testTarget,
		Name:            "missing",
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("err = %v, want NotFound", err)
	}
}

// TestGet_notFoundMissingFromSnapshot covers a target that does have a
// TargetSnapshot, but the requested intent isn't (yet, or anymore) one of
// its entries.
func TestGet_notFoundMissingFromSnapshot(t *testing.T) {
	snapshot := mkTargetSnapshot(testNamespace, testTarget, map[string]configv1alpha1.SensitiveConfigSpec{
		"other-cfg": mkSnapshotEntry(t, newTestKeyRing(t), nil, nil),
	})
	s := newTestServer(t, snapshot)

	_, err := s.Get(context.Background(), &config_read.GetConfigRequest{
		TargetNamespace: testNamespace,
		TargetName:      testTarget,
		Name:            "cfg1",
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("err = %v, want NotFound", err)
	}
}

func TestGet_missingArgs(t *testing.T) {
	s := newTestServer(t)
	_, err := s.Get(context.Background(), &config_read.GetConfigRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("err = %v, want InvalidArgument", err)
	}
}

// TestGet_fieldMapping verifies the field mapping straight off the
// TargetSnapshot entry's SensitiveConfigSpec (non_revertive is the inverse
// of Revertive; orphan reflects the deletion policy; sensitive_paths comes
// off the same spec — no separate joined object).
func TestGet_fieldMapping(t *testing.T) {
	kr := newTestKeyRing(t)
	entrySpec := mkSnapshotEntry(t, kr, nil, func(spec *configv1alpha1.SensitiveConfigSpec) {
		spec.Revertive = ptr.To(false)
		spec.Lifecycle = &configv1alpha1.Lifecycle{DeletionPolicy: configv1alpha1.DeletionOrphan}
		spec.SensitivePaths = []string{"/interface/name"}
	})
	snapshot := mkTargetSnapshot(testNamespace, testTarget, map[string]configv1alpha1.SensitiveConfigSpec{
		"cfg1": entrySpec,
	})
	s := newTestServerWithKeyRing(t, kr, snapshot)

	rsp, err := s.Get(context.Background(), &config_read.GetConfigRequest{
		TargetNamespace: testNamespace,
		TargetName:      testTarget,
		Name:            "cfg1",
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	entry := rsp.GetConfig()
	if !entry.GetNonRevertive() {
		t.Errorf("non_revertive = false, want true (Revertive=false)")
	}
	if !entry.GetOrphan() {
		t.Errorf("orphan = false, want true (DeletionOrphan)")
	}
	if len(entry.GetSensitivePaths()) != 1 {
		t.Fatalf("sensitive_paths = %+v, want 1 entry", entry.GetSensitivePaths())
	}
}

func TestGet_defaultsRevertiveTrue(t *testing.T) {
	kr := newTestKeyRing(t)
	entrySpec := mkSnapshotEntry(t, kr, nil, nil) // Revertive unset
	snapshot := mkTargetSnapshot(testNamespace, testTarget, map[string]configv1alpha1.SensitiveConfigSpec{
		"cfg1": entrySpec,
	})
	s := newTestServerWithKeyRing(t, kr, snapshot)

	rsp, err := s.Get(context.Background(), &config_read.GetConfigRequest{
		TargetNamespace: testNamespace,
		TargetName:      testTarget,
		Name:            "cfg1",
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rsp.GetConfig().GetNonRevertive() {
		t.Errorf("non_revertive = true, want false (Revertive defaults to true)")
	}
}

// ── List ─────────────────────────────────────────────────────────────────────

func TestList_scopedToTarget(t *testing.T) {
	cfg1 := mkTargetConfig("cfg1", testNamespace, testTarget, nil)
	cfg2 := mkTargetConfig("cfg2", testNamespace, testTarget, nil)
	other := mkTargetConfig("cfg3", testNamespace, "other-target", nil)
	s := newTestServer(t, cfg1, cfg2, other)

	rsp, err := s.List(context.Background(), &config_read.ListConfigRequest{
		TargetNamespace: testNamespace,
		TargetName:      testTarget,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := map[string]bool{}
	for _, e := range rsp.GetConfig() {
		got[e.GetName()] = true
	}
	if len(got) != 2 || !got["cfg1"] || !got["cfg2"] {
		t.Fatalf("List returned %+v, want exactly cfg1 and cfg2", got)
	}
}

func TestList_empty(t *testing.T) {
	s := newTestServer(t)
	rsp, err := s.List(context.Background(), &config_read.ListConfigRequest{
		TargetNamespace: testNamespace,
		TargetName:      testTarget,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rsp.GetConfig()) != 0 {
		t.Fatalf("List = %+v, want empty", rsp.GetConfig())
	}
}

func TestList_missingArgs(t *testing.T) {
	s := newTestServer(t)
	_, err := s.List(context.Background(), &config_read.ListConfigRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("err = %v, want InvalidArgument", err)
	}
}

func TestList_joinsEachSensitiveConfigByName(t *testing.T) {
	cfg1 := mkTargetConfig("cfg1", testNamespace, testTarget, nil)
	cfg2 := mkTargetConfig("cfg2", testNamespace, testTarget, nil)
	sc1 := &configv1alpha1.SensitiveConfig{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: "cfg1"},
		Spec:       configv1alpha1.SensitiveConfigSpec{SensitivePaths: []string{"/interface/name"}},
	}
	s := newTestServer(t, cfg1, cfg2, sc1)

	rsp, err := s.List(context.Background(), &config_read.ListConfigRequest{
		TargetNamespace: testNamespace,
		TargetName:      testTarget,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byName := map[string]*config_read.ConfigEntry{}
	for _, e := range rsp.GetConfig() {
		byName[e.GetName()] = e
	}
	if len(byName["cfg1"].GetSensitivePaths()) != 1 {
		t.Errorf("cfg1 sensitive_paths = %+v, want 1", byName["cfg1"].GetSensitivePaths())
	}
	if len(byName["cfg2"].GetSensitivePaths()) != 0 {
		t.Errorf("cfg2 sensitive_paths = %+v, want 0 (no SensitiveConfig)", byName["cfg2"].GetSensitivePaths())
	}
}
