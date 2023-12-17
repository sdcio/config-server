package client

import (
	"context"
	"fmt"
	"time"

	"github.com/henderiw/logger/log"
	sdcpb "github.com/iptecharch/sdc-protos/sdcpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context)
	GetAddress() string
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

func (r *client) Stop(ctx context.Context) {
	log := log.FromContext(ctx).With("address", r.cfg.Address)
	log.Info("stopping...")
	r.conn.Close()
	r.cancel()
}

func (r *client) Start(ctx context.Context) error {
	log := log.FromContext(ctx).With("address", r.cfg.Address)
	log.Info("starting...")

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var err error
	r.conn, err = grpc.DialContext(ctx, r.cfg.Address,
		grpc.WithBlock(),
		grpc.WithTransportCredentials(
			insecure.NewCredentials(),
		),
	)
	if err != nil {
		return err
	}

	//defer conn.Close()
	r.schemaclient = sdcpb.NewSchemaServerClient(r.conn)
	log.Info("started...")
	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Info("stopped...")
				return
			}
		}
	}()
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
