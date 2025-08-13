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
	"time"

	"github.com/henderiw/logger/log"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/connectivity"
)

type Client interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context)
	GetAddress() string
	IsConnectionReady() bool
	IsConnected() bool
	sdcpb.SchemaServerClient
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

func New(cfg *Config) (Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("cannot create client with empty config")
	}
	if cfg.Address == "" {
		return nil, fmt.Errorf("cannot create client with an empty address")
	}

	return &client{
		cfg: cfg,
	}, nil
}

type client struct {
	cfg          *Config
	cancel       context.CancelFunc
	conn         *grpc.ClientConn
	schemaclient sdcpb.SchemaServerClient
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
	r.conn.Close()
	r.cancel()
}

func (r *client) Start(ctx context.Context) error {
	log := log.FromContext(ctx).With("address", r.cfg.Address)
	log.Info("starting...")

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Create the ClientConn
    conn, err := grpc.NewClient(
        r.cfg.Address,
        grpc.WithTransportCredentials(insecure.NewCredentials()),
    )
    if err != nil {
        return err
    }

	// Proactively start connecting and wait for Ready (or timeout/cancel)
    conn.Connect()
    for {
        s := conn.GetState()
        if s == connectivity.Ready {
            break
        }
        if !conn.WaitForStateChange(dialCtx, s) {
            // context expired or canceled
            _ = conn.Close()
            return fmt.Errorf("gRPC connect timeout; last state: %s", s.String())
        }
    }

	r.conn = conn
	r.schemaclient = sdcpb.NewSchemaServerClient(r.conn)

	// Create a long-lived cancel just for Stop()
    runCtx, stop := context.WithCancel(context.Background())
    r.cancel = stop
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

/*
func (r *client) getGRPCOpts() ([]grpc.DialOption, error) {
	var opts []grpc.DialOption
	fmt.Printf("grpc client config: %v\n", r.cfg)
	if r.cfg.Insecure {
		//opts = append(opts, grpc.WithInsecure())
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		tlsConfig, err := r.newTLS()
		if err != nil {
			return nil, err
		}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	}
	return opts, nil
}

func (r *client) newTLS() (*tls.Config, error) {
	tlsConfig := &tls.Config{
		Renegotiation:      tls.RenegotiateNever,
		InsecureSkipVerify: r.cfg.SkipVerify,
	}
	//err := loadCerts(tlsConfig)
	//if err != nil {
	//	return nil, err
	//}
	return tlsConfig, nil
}

func loadCerts(tlscfg *tls.Config) error {
	if c.TLSCert != "" && c.TLSKey != "" {
		certificate, err := tls.LoadX509KeyPair(*c.TLSCert, *c.TLSKey)
		if err != nil {
			return err
		}
		tlscfg.Certificates = []tls.Certificate{certificate}
		tlscfg.BuildNameToCertificate()
	}
	if c.TLSCA != nil && *c.TLSCA != "" {
		certPool := x509.NewCertPool()
		caFile, err := ioutil.ReadFile(*c.TLSCA)
		if err != nil {
			return err
		}
		if ok := certPool.AppendCertsFromPEM(caFile); !ok {
			return errors.New("failed to append certificate")
		}
		tlscfg.RootCAs = certPool
	}
	return nil
}
*/
