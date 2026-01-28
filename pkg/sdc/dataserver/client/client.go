/*
Copyright 2024 Nokia.

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
	"os"
	"time"

	"github.com/henderiw/logger/log"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	//"google.golang.org/grpc/keepalive"
)

const dataServerAddress = "data-server.sdc-system.svc.cluster.local:56000"
const localDataServerAddress = "localhost:56000"

func GetDataServerAddress() string {
	if address, found := os.LookupEnv("SDC_DATA_SERVER"); found {
		return address
	}
	return dataServerAddress
}

func GetLocalDataServerAddress() string {
	if address, found := os.LookupEnv("SDC_DATA_SERVER"); found {
		return address
	}
	return localDataServerAddress
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

func defaukltConfig(cfg *Config) {
	if cfg == nil {
		cfg = &Config{Address: GetDataServerAddress()}
	}
	if cfg.Address == "" {
		cfg.Address = GetDataServerAddress()
	}
}

/*
Example: One-shot use

	cfg := &client.Config{
		Address:  "data-server.sdc-system.svc.cluster.local:50052",
		Insecure: true,
	}

	err := client.OneShot(ctx, cfg, func(ctx context.Context, c sdcpb.DataServerClient) error {
		resp, err := c.ListDataStore(ctx, &sdcpb.ListDataStoreRequest{})
		if err != nil {
			return err
		}
		fmt.Printf("Got %d datastores\n", len(resp.GetDatastores()))
		return nil
	})
	if err != nil {
		// handle error
	}
*/

// OneShot runs a function with a short-lived DataServerClient.
// It dials, runs fn, and always closes the connection.
func OneShot(
	ctx context.Context,
	cfg *Config,
	fn func(ctx context.Context, c sdcpb.DataServerClient) error,
) error {
	defaukltConfig(cfg)
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

	c := sdcpb.NewDataServerClient(conn)
	return fn(ctx, c)
}

/*
Example: Ephemeral client

	cfg := &client.Config{
		Address:  "data-server.sdc-system.svc.cluster.local:50052",
		Insecure: true,
	}

	c, closeFn, err := client.NewEphemeral(ctx, cfg)
	if err != nil {
		// handle
		return
	}
	defer closeFn()

	resp, err := c.ListIntent(ctx, &sdcpb.ListIntentRequest{})
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
) (sdcpb.DataServerClient, func() error, error) {
	defaukltConfig(cfg)
	conn, err := dial(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}

	c := sdcpb.NewDataServerClient(conn)
	closeFn := func() error {
		return conn.Close()
	}
	return c, closeFn, nil
}

// Client is a long-lived DataServer client (for controllers, daemons, etc.).
type Client interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context)
	GetAddress() string
	IsConnectionReady() bool
	IsConnected() bool
	ConnState() connectivity.State
	WaitForStateChange(ctx context.Context, source connectivity.State) bool
	Connect()
	sdcpb.DataServerClient
}

func New(cfg *Config) (Client, error) {
	defaukltConfig(cfg)
	// default cancel is a no-op so Stop() is always safe
	return &client{cfg: cfg, cancel: func() {}}, nil
}

type client struct {
	cfg      *Config
	cancel   context.CancelFunc
	conn     *grpc.ClientConn
	dsclient sdcpb.DataServerClient
}

const DSConnectionStatusNotConnected = "DATASERVER_NOT_CONNECTED"

// dial creates a gRPC connection with a timeout and proper options,
// using the modern grpc.NewClient API.
func dial(ctx context.Context, cfg *Config) (*grpc.ClientConn, error) {
	defaukltConfig(cfg)

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// TODO: add TLS / max msg size based on cfg here.
	}

	conn, err := grpc.NewClient(cfg.Address, opts...)
	if err != nil {
		return nil, fmt.Errorf("create data-server client %q: %w", cfg.Address, err)
	}

	// Proactively start connecting and wait for Ready (or timeout/cancel).
	conn.Connect()
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			return conn, nil
		}
		if !conn.WaitForStateChange(dialCtx, state) {
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

func (r *client) ConnState() connectivity.State {
	if r.conn == nil {
		return connectivity.Shutdown
	}
	return r.conn.GetState()
}

func (r *client) WaitForStateChange(ctx context.Context, s connectivity.State) bool {
	if r.conn == nil {
		return false
	}
	return r.conn.WaitForStateChange(ctx, s)
}

func (r *client) Connect() {
	if r.conn != nil {
		r.conn.Connect()
	}
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

	startCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var err error
	r.conn, err = grpc.NewClient(r.cfg.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             5 * time.Second,
			PermitWithoutStream: false,
		}),
	)
	if err != nil {
		return err
	}

	// Wait until the channel is Ready (or timeout)
	if err := waitForReady(startCtx, r.conn); err != nil {
		_ = r.conn.Close()
		r.conn = nil
		return fmt.Errorf("connect %q: %w", r.cfg.Address, err)
	}
	r.dsclient = sdcpb.NewDataServerClient(r.conn)

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

// ---------- RPC wrappers for sdcpb.DataServerClient ----------

func (r *client) ListDataStore(ctx context.Context, in *sdcpb.ListDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.ListDataStoreResponse, error) {
	return r.dsclient.ListDataStore(ctx, in, opts...)
}

func (r *client) GetDataStore(ctx context.Context, in *sdcpb.GetDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.GetDataStoreResponse, error) {
	return r.dsclient.GetDataStore(ctx, in, opts...)
}

func (r *client) CreateDataStore(ctx context.Context, in *sdcpb.CreateDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.CreateDataStoreResponse, error) {
	return r.dsclient.CreateDataStore(ctx, in, opts...)
}

func (r *client) DeleteDataStore(ctx context.Context, in *sdcpb.DeleteDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.DeleteDataStoreResponse, error) {
	return r.dsclient.DeleteDataStore(ctx, in, opts...)
}

//func (r *client) SetData(ctx context.Context, in *sdcpb.SetDataRequest, opts ...grpc.CallOption) (*sdcpb.SetDataResponse, error) {
//	return r.dsclient.SetData(ctx, in, opts...)
//}

func (r *client) GetIntent(ctx context.Context, in *sdcpb.GetIntentRequest, opts ...grpc.CallOption) (*sdcpb.GetIntentResponse, error) {
	return r.dsclient.GetIntent(ctx, in, opts...)
}

func (r *client) ListIntent(ctx context.Context, in *sdcpb.ListIntentRequest, opts ...grpc.CallOption) (*sdcpb.ListIntentResponse, error) {
	return r.dsclient.ListIntent(ctx, in, opts...)
}

func (r *client) WatchDeviations(ctx context.Context, in *sdcpb.WatchDeviationRequest, opts ...grpc.CallOption) (sdcpb.DataServer_WatchDeviationsClient, error) {
	return r.dsclient.WatchDeviations(ctx, in, opts...)
}

func (r *client) TransactionSet(ctx context.Context, in *sdcpb.TransactionSetRequest, opts ...grpc.CallOption) (*sdcpb.TransactionSetResponse, error) {
	return r.dsclient.TransactionSet(ctx, in, opts...)
}

func (r *client) TransactionConfirm(ctx context.Context, in *sdcpb.TransactionConfirmRequest, opts ...grpc.CallOption) (*sdcpb.TransactionConfirmResponse, error) {
	return r.dsclient.TransactionConfirm(ctx, in, opts...)
}

func (r *client) TransactionCancel(ctx context.Context, in *sdcpb.TransactionCancelRequest, opts ...grpc.CallOption) (*sdcpb.TransactionCancelResponse, error) {
	return r.dsclient.TransactionCancel(ctx, in, opts...)
}

func (r *client) BlameConfig(ctx context.Context, in *sdcpb.BlameConfigRequest, opts ...grpc.CallOption) (*sdcpb.BlameConfigResponse, error) {
	return r.dsclient.BlameConfig(ctx, in, opts...)
}

func waitForReady(ctx context.Context, conn *grpc.ClientConn) error {
	conn.Connect()

	for {
		st := conn.GetState()
		switch st {
		case connectivity.Ready:
			return nil
		case connectivity.Shutdown:
			return fmt.Errorf("grpc shutdown")
		default:
			// Idle / Connecting / TransientFailure => keep waiting
		}

		if !conn.WaitForStateChange(ctx, st) {
			return ctx.Err() // deadline or cancellation
		}
	}
}
