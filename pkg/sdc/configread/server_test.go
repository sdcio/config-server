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
	"net"
	"testing"
	"time"

	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/sdcio/sdc-protos/config_read"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestNewServer_nilKeyRing locks NewServer's fail-fast contract: a Server
// backed by a nil KeyRing must never start, since every entry it later reads
// (TargetSnapshot.Spec.Configs) needs KeyRing.Decrypt.
func TestNewServer_nilKeyRing(t *testing.T) {
	_, err := NewServer(&Config{
		Address: "127.0.0.1:0",
		Client:  fake.NewClientBuilder().Build(),
		KeyRing: nil,
	})
	if err == nil {
		t.Fatal("NewServer with nil KeyRing: want error, got nil")
	}
}

// TestServer_liveRoundTrip starts the real gRPC server on a free localhost
// port and drives it through the generated client, end to end — listener
// setup, registration, and the wire contract all in one shot.
func TestServer_liveRoundTrip(t *testing.T) {
	sch := runtime.NewScheme()
	if err := configv1alpha1.AddToScheme(sch); err != nil {
		t.Fatalf("add configv1alpha1 to scheme: %v", err)
	}
	kr := newTestKeyRing(t)
	entrySpec := mkSnapshotEntry(t, kr, []config.ConfigBlob{
		{Path: "/system", Value: runtime.RawExtension{Raw: []byte(`{"hostname":"router1"}`)}},
	}, nil)
	snapshot := mkTargetSnapshot(testNamespace, testTarget, map[string]configv1alpha1.SensitiveConfigSpec{
		"cfg1": entrySpec,
	})
	fakeClient := fake.NewClientBuilder().WithScheme(sch).WithObjects(snapshot).Build()

	addr := freeLocalAddr(t)
	srv, err := NewServer(&Config{Address: addr, Client: fakeClient, KeyRing: kr})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()
	waitForDial(t, addr, 2*time.Second)

	cc, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = cc.Close() }()

	cl := config_read.NewConfigReadServiceClient(cc)
	rsp, err := cl.Get(context.Background(), &config_read.GetConfigRequest{
		TargetNamespace: testNamespace,
		TargetName:      testTarget,
		Name:            "cfg1",
	})
	if err != nil {
		t.Fatalf("Get over the wire: %v", err)
	}
	if rsp.GetConfig().GetName() != "cfg1" {
		t.Fatalf("got %+v, want name=cfg1", rsp.GetConfig())
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Start returned error after cancel: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not stop after context cancel")
	}
}

// freeLocalAddr grabs an OS-assigned free port, then releases it — a small,
// accepted race in exchange for not needing to plumb the listener itself
// through Start.
func freeLocalAddr(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve free port: %v", err)
	}
	addr := lis.Addr().String()
	_ = lis.Close()
	return addr
}

func waitForDial(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server did not start listening on %s within %s", addr, timeout)
}
