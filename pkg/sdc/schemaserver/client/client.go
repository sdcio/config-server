/*
Copyright 2025 Nokia.

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

package client

import (
	"context"
	"fmt"
	"time"
	"os"

	"github.com/henderiw/logger/log"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

const (
    defaultSchemaServerService = "schema-server"
    defaultSchemaServerPort    = "56000"
    defaultNamespace           = "sdc-system"
)

func GetSchemaServerAddress() string {
	if address, found := os.LookupEnv("SDC_SCHEMA_SERVER"); found {
		return address
	}
	if address, found := os.LookupEnv("SDC_DATA_SERVER"); found {
		return address
	}
	// Build from components
    ns := envOrDefault("POD_NAMESPACE", defaultNamespace)
    svc := envOrDefault("SDC_SCHEMA_SERVER_SERVICE", defaultSchemaServerService)
    port := envOrDefault("SDC_SCHEMA_SERVER_PORT", defaultSchemaServerPort)

    return fmt.Sprintf("%s.%s.svc.cluster.local:%s", svc, ns, port)
}

func envOrDefault(key, fallback string) string {
    if v, ok := os.LookupEnv(key); ok && v != "" {
        return v
    }
    return fallback
}

type Config struct {
	Address    string
	Username   string
	Password   string
	Proxy      bool
	NoTLS      bool
	TLSCA      string
	TLSCert    string
	TLSKey     string
	SkipVerify bool
	Insecure   bool
	MaxMsgSize int
}

func defaultConfig(cfg *Config) {
	if cfg == nil {
		cfg = &Config{Address: GetSchemaServerAddress()}
	}
	if cfg.Address == "" {
		cfg.Address = GetSchemaServerAddress()
	}
}

/*
Example: One-shot use

	cfg := &client.Config{
		Address:  "schema-server.sdc-system.svc.cluster.local:50051",
		Insecure: true,
	}

	err := client.OneShot(ctx, cfg, func(ctx context.Context, c sdcpb.SchemaServerClient) error {
		resp, err := c.ListSchema(ctx, &sdcpb.ListSchemaRequest{})
		if err != nil {
			return err
		}
		fmt.Printf("Got %d schemas\n", len(resp.GetSchemas()))
		return nil
	})
	if err != nil {
		// handle error
	}
*/

// OneShot runs a function with a short-lived SchemaServerClient.
// It dials, runs fn, and always closes the connection.
func OneShot(
	ctx context.Context,
	cfg *Config,
	fn func(ctx context.Context, c sdcpb.SchemaServerClient) error,
) error {
	defaultConfig(cfg)
	conn, err := dial(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		if err := conn.Close(); err != nil {
			// You can use your preferred logging framework here
			fmt.Printf("failed to close connection: %v\n", err)
		}
	}()

	c := sdcpb.NewSchemaServerClient(conn)
	return fn(ctx, c)
}

/*
Example: Ephemeral client

	cfg := &client.Config{
		Address:  "schema-server.sdc-system.svc.cluster.local:50051",
		Insecure: true,
	}

	c, closeFn, err := client.NewEphemeral(ctx, cfg)
	if err != nil {
		// handle
		return
	}
	defer closeFn()

	resp, err := c.GetSchemaDetails(ctx, &sdcpb.GetSchemaDetailsRequest{  })
	if err != nil {
		// handle
	}
	_ = resp
*/

// NewEphemeral returns a short-lived client and a close function.
// Call closeFn() when you're done.
func NewEphemeral(
	ctx context.Context,
	cfg *Config,
) (sdcpb.SchemaServerClient, func() error, error) {
	defaultConfig(cfg)
	conn, err := dial(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}

	c := sdcpb.NewSchemaServerClient(conn)
	closeFn := func() error {
		return conn.Close()
	}
	return c, closeFn, nil
}

// Client is a long-lived SchemaServer client (for controllers, daemons, etc.).
type Client interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context)
	GetAddress() string
	IsConnectionReady() bool
	IsConnected() bool
	sdcpb.SchemaServerClient
}


func New(cfg *Config) (Client, error) {
	defaultConfig(cfg)
	// default cancel is a no-op so Stop() is always safe
	return &client{cfg: cfg, cancel: func() {}}, nil
}

type client struct {
	cfg          *Config
	cancel       context.CancelFunc
	conn         *grpc.ClientConn
	schemaclient sdcpb.SchemaServerClient
}

const DSConnectionStatusNotConnected = "DATASERVER_NOT_CONNECTED"

// dial creates a gRPC connection with a timeout and proper options,
// using the modern grpc.NewClient API.
func dial(ctx context.Context, cfg *Config) (*grpc.ClientConn, error) {
	defaultConfig(cfg)

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// TODO: add TLS / max msg size based on cfg here.
	}

	conn, err := grpc.NewClient(cfg.Address, opts...)
	if err != nil {
		return nil, fmt.Errorf("create schema-server client %q: %w", cfg.Address, err)
	}

	// Proactively start connecting and wait for Ready (or timeout/cancel).
	conn.Connect()
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			return conn, nil
		}
		if !conn.WaitForStateChange(dialCtx, state) {
			// context expired or canceled
			_ = conn.Close()
			return nil, fmt.Errorf("gRPC connect timeout to %q; last state: %s", cfg.Address, state.String())
		}
	}
}

func (r *client) IsConnectionReady() bool {
	return r.conn != nil && r.conn.GetState() == connectivity.Ready
}

func (r *client) IsConnected() bool {
	return r.conn != nil && r.conn.GetState() != connectivity.Shutdown
}

func (r *client) Stop(ctx context.Context) {
	log := log.FromContext(ctx).With("address", r.cfg.Address)
	log.Info("stopping...")

	if r.conn != nil {
		if err := r.conn.Close(); err != nil {
			log.Error("close error", "err", err)
		}
	}
	if r.cancel != nil {
		r.cancel()
	}
}

func (r *client) Start(ctx context.Context) error {
	log := log.FromContext(ctx).With("address", r.cfg.Address)
	log.Info("starting...")

	conn, err := dial(ctx, r.cfg)
	if err != nil {
		return err
	}

	r.conn = conn
	r.schemaclient = sdcpb.NewSchemaServerClient(r.conn)

	// Long-lived cancel for Stop()
	runCtx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go func() {
		<-runCtx.Done()
		log.Info("stopped...")
	}()

	log.Info("started...")
	return nil
}

func (r *client) GetAddress() string {
	return r.cfg.Address
}

// ---------- RPC wrappers for sdcpb.SchemaServerClient ----------

// returns schema name, vendor, version, and files path(s)
func (r *client) GetSchemaDetails(ctx context.Context, in *sdcpb.GetSchemaDetailsRequest, opts ...grpc.CallOption) (*sdcpb.GetSchemaDetailsResponse, error) {
	return r.schemaclient.GetSchemaDetails(ctx, in, opts...)
}

// lists known schemas with name, vendor, version and status
func (r *client) ListSchema(ctx context.Context, in *sdcpb.ListSchemaRequest, opts ...grpc.CallOption) (*sdcpb.ListSchemaResponse, error) {
	return r.schemaclient.ListSchema(ctx, in, opts...)
}

// returns the schema of an item identified by a gNMI-like path
func (r *client) GetSchema(ctx context.Context, in *sdcpb.GetSchemaRequest, opts ...grpc.CallOption) (*sdcpb.GetSchemaResponse, error) {
	return r.schemaclient.GetSchema(ctx, in, opts...)
}

// creates a schema
func (r *client) CreateSchema(ctx context.Context, in *sdcpb.CreateSchemaRequest, opts ...grpc.CallOption) (*sdcpb.CreateSchemaResponse, error) {
	return r.schemaclient.CreateSchema(ctx, in, opts...)
}

// trigger schema reload
func (r *client) ReloadSchema(ctx context.Context, in *sdcpb.ReloadSchemaRequest, opts ...grpc.CallOption) (*sdcpb.ReloadSchemaResponse, error) {
	return r.schemaclient.ReloadSchema(ctx, in, opts...)
}

// delete a schema
func (r *client) DeleteSchema(ctx context.Context, in *sdcpb.DeleteSchemaRequest, opts ...grpc.CallOption) (*sdcpb.DeleteSchemaResponse, error) {
	return r.schemaclient.DeleteSchema(ctx, in, opts...)
}

// client stream RPC to upload yang files to the server:
// - uses CreateSchema as a first message
// - then N intermediate UploadSchemaFile, initial, bytes, hash for each file
// - and ends with an UploadSchemaFinalize{}
func (r *client) UploadSchema(ctx context.Context, opts ...grpc.CallOption) (sdcpb.SchemaServer_UploadSchemaClient, error) {
	return r.schemaclient.UploadSchema(ctx, opts...)
}

// ToPath converts a list of items into a schema.proto.Path
func (r *client) ToPath(ctx context.Context, in *sdcpb.ToPathRequest, opts ...grpc.CallOption) (*sdcpb.ToPathResponse, error) {
	return r.schemaclient.ToPath(ctx, in, opts...)
}

// ExpandPath returns a list of sub paths given a single path
func (r *client) ExpandPath(ctx context.Context, in *sdcpb.ExpandPathRequest, opts ...grpc.CallOption) (*sdcpb.ExpandPathResponse, error) {
	return r.schemaclient.ExpandPath(ctx, in, opts...)
}

// GetSchemaElements returns the schema of each path element
func (r *client) GetSchemaElements(ctx context.Context, in *sdcpb.GetSchemaRequest, opts ...grpc.CallOption) (sdcpb.SchemaServer_GetSchemaElementsClient, error) {
	return r.schemaclient.GetSchemaElements(ctx, in, opts...)
}