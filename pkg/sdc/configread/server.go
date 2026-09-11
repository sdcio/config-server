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

// Package configread implements config_read.ConfigSnapshotService: a unary,
// localhost-bound Get-by-name/List-by-target/Modify/Delete API over
// config-server's TargetSnapshot resource. Reads (Get/List) use an uncached
// API-server reader (mgr.GetAPIReader()) so they are immediately consistent
// with Modify/Delete writes, which also go directly to the API server via
// merge-patch. This eliminates the informer-propagation lag that would
// otherwise let LoadAllButRunningIntents see a stale snapshot immediately
// after a write — see pkg/cache/docs/adr/0003-... (data-server repo).
package configread

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/pkg/keyring"
	"github.com/sdcio/sdc-protos/config_read"
	"google.golang.org/grpc"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const defaultPort = "56010"

// GetLocalAddress returns the localhost bind/dial address for this service.
// Configurable via SDC_CONFIG_READ_PORT; always 127.0.0.1-bound — this is a
// same-pod read surface, never meant to cross a network boundary.
func GetLocalAddress() string {
	return fmt.Sprintf("127.0.0.1:%s", envOrDefault("SDC_CONFIG_READ_PORT", defaultPort))
}

func envOrDefault(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// Config carries what a Server needs to construct.
type Config struct {
	// Address is the localhost bind address, e.g. "127.0.0.1:56010".
	Address string
	// Client is the manager's cached client — used for writes (Patch/Create)
	// against the API server.
	Client client.Client
	// APIReader is an uncached reader that goes directly to the API server —
	// used for Get/List reads so they are immediately consistent with writes
	// made by Modify/Delete in the same or a prior RPC.
	APIReader client.Reader
	// KeyRing decrypts TargetSnapshot entries' EncryptedPayload. Required —
	// NewServer fails fast if nil rather than letting Get/List fail lazily
	// on first call.
	KeyRing *keyring.KeyRing
}

// Server implements config_read.ConfigSnapshotServiceServer over the
// API server directly for both reads (Get/List via APIReader) and writes
// (Modify/Delete via Client). Using an uncached reader for Get/List ensures
// read-after-write consistency: a Modify/Delete patch lands on the API server
// and the immediately following List sees it, with no informer-propagation
// lag in between.
type Server struct {
	config_read.UnimplementedConfigSnapshotServiceServer

	address   string
	client    client.Client
	apiReader client.Reader
	keyRing   *keyring.KeyRing
}

// NewServer constructs a Server. Call AddToManager to start it alongside the
// manager.
func NewServer(cfg *Config) (*Server, error) {
	if cfg.KeyRing == nil {
		return nil, fmt.Errorf("KeyRing is nil: required for TargetSnapshot decryption")
	}
	if cfg.APIReader == nil {
		return nil, fmt.Errorf("APIReader is nil: required for consistent read-after-write")
	}
	return &Server{address: cfg.Address, client: cfg.Client, apiReader: cfg.APIReader, keyRing: cfg.KeyRing}, nil
}

// AddToManager registers the server as a controller-runtime Runnable so it
// starts (and stops) alongside the manager's own lifecycle.
func (s *Server) AddToManager(mgr ctrl.Manager) error {
	return mgr.Add(s)
}

// Start implements controller-runtime's manager.Runnable.
func (s *Server) Start(ctx context.Context) error {
	l := log.FromContext(ctx).With("component", "configReadServer", "address", s.address)

	lis, err := net.Listen("tcp", s.address)
	if err != nil {
		return fmt.Errorf("configReadServer: listen on %s: %w", s.address, err)
	}
	defer func() { _ = lis.Close() }()

	grpcServer := grpc.NewServer()
	config_read.RegisterConfigSnapshotServiceServer(grpcServer, s)

	errCh := make(chan error, 1)
	go func() {
		errCh <- grpcServer.Serve(lis)
	}()
	l.Info("configReadServer started")

	select {
	case <-ctx.Done():
		l.Info("configReadServer stopping")
		grpcServer.GracefulStop()
		return nil
	case err := <-errCh:
		return err
	}
}
