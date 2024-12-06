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

package prometheusserver

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sdcio/config-server/pkg/target"
	certutil "k8s.io/client-go/util/cert"
)

type Config struct {
	Address     string
	TargetStore storebackend.Storer[*target.Context]
}

func NewServer(c *Config) *PrometheusServer {
	return &PrometheusServer{
		address:     c.Address,
		targetStore: c.TargetStore,
	}
}

type PrometheusServer struct {
	address     string
	targetStore storebackend.Storer[*target.Context]
	// dynamic
	cancel func()
	server *http.Server
}

func (r *PrometheusServer) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
}

func (r *PrometheusServer) Start(ctx context.Context) error {
	log := log.FromContext(ctx).With("name", "prometheusserver", "address", r.address)
	ctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel

	// create prometheus registry
	registry := prometheus.NewRegistry()
	if err := registry.Register(r); err != nil {
		return err
	}

	// create http server
	promHandler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{ErrorHandling: promhttp.ContinueOnError})
	mux := http.NewServeMux()
	mux.Handle("/metrics", promHandler)
	mux.Handle("/", new(healthHandler))

	r.server = &http.Server{Addr: r.address, Handler: mux}

	listener, err := net.Listen("tcp", r.address)
	if err != nil {
		log.Error("prometheusserver cannot listen on address", "error", err)
		return err
	}
	defer listener.Close()

	go func() {
		if err := r.server.Serve(listener); err != nil {
			log.Error("grpc server serve", "error", err)
		}
	}()
	log.Info("prometheusserver started")

	for range ctx.Done() {
		log.Info("prometheusserver stopped...")
		r.cancel()
	}
	return nil
}

func (r *PrometheusServer) createListener(secureServing bool) (net.Listener, error) {
	if !secureServing {
		return net.Listen("tcp", r.address)
	}

	// Note: Using self-signed certificates here should be good enough. It's just important that we
	// encrypt the communication.
	cert, key, err := certutil.GenerateSelfSignedCertKeyWithFixtures("localhost", []net.IP{{127, 0, 0, 1}}, nil, "")
	if err != nil {
		return nil, fmt.Errorf("failed to generate self-signed certificate for prometheusServer: %w", err)
	}

	keyPair, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return nil, fmt.Errorf("failed to create self-signed key pair for prometheusServer: %w", err)
	}

	// Configure the TLS settings
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{keyPair},
	}

	return tls.Listen("tcp", r.address, tlsConfig)
}
