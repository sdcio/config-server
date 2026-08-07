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

// Package configread implements config_read.ConfigReadService: a unary,
// localhost-bound Get-by-name/List-by-target read API over config-server's
// Config (+ joined SensitiveConfig) resources, backed entirely by the
// colocated controller's existing watch-synced informer cache — the same one
// targetmanager.ConfigManager.ListConfigsPerTarget already reads from. No new
// watch, no new store, no new trust boundary: see
// pkg/cache/docs/adr/0001-config-server-backed-cache-client.md (data-server
// repo) for the contract this serves.
package configread

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/henderiw/logger/log"
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
	// Client is the manager's cached client — the same watch-synced informer
	// cache Transactor/ConfigManager already read from.
	Client client.Client
}

// Server implements config_read.ConfigReadServiceServer over the colocated
// controller's existing informer cache. It is a controller-runtime Runnable
// (via AddToManager), not a CRD reconciler: it reconciles nothing and owns no
// watch of its own.
type Server struct {
	config_read.UnimplementedConfigReadServiceServer

	address string
	client  client.Client
}

// NewServer constructs a Server. Call AddToManager to start it alongside the
// manager.
func NewServer(cfg *Config) *Server {
	return &Server{address: cfg.Address, client: cfg.Client}
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
	defer lis.Close()

	grpcServer := grpc.NewServer()
	config_read.RegisterConfigReadServiceServer(grpcServer, s)

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
