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
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

type Client interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context)
	GetAddress() string
	IsConnectionReady() bool
	IsConnected() bool
	sdcpb.DataServerClient
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
	cfg      *Config
	cancel   context.CancelFunc
	conn     *grpc.ClientConn
	dsclient sdcpb.DataServerClient
}

const DSConnectionStatusNotConnected = "DATASERVER_NOT_CONNECTED"

func (r *client) IsConnectionReady() bool {
	if r.conn == nil {
		return false
	}
	return r.conn.GetState() == connectivity.Ready
}

func (r *client) IsConnected() bool {
	return r.conn != nil 
}

func (r *client) Stop(ctx context.Context) {
	log := log.FromContext(ctx).With("address", r.cfg.Address)
	log.Info("stopping...")
	if err := r.conn.Close(); err != nil {
		log.Error("close error", "err", err)
	}
	r.cancel()
}

func (r *client) Start(ctx context.Context) error {
	log := log.FromContext(ctx).With("address", r.cfg.Address)
	log.Info("starting...")

	_, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var err error
	r.conn, err = grpc.NewClient(r.cfg.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return err
	}

	//defer conn.Close()
	r.dsclient = sdcpb.NewDataServerClient(r.conn)
	log.Info("started...")
	return nil
}

func (r *client) GetAddress() string {
	return r.cfg.Address
}

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
