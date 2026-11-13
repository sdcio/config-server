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

//go:generate apiserver-runtime-gen
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	//"time"

	"github.com/go-logr/logr"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/internal/verbosity"
	"github.com/sdcio/config-server/pkg/output/prometheusserver"
	"github.com/sdcio/config-server/pkg/reconcilers"
	_ "github.com/sdcio/config-server/pkg/reconcilers/all"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	targetmanager "github.com/sdcio/config-server/pkg/sdc/target/manager"
	dsconfig "github.com/sdcio/data-server/pkg/config"
	"github.com/sdcio/data-server/pkg/core"
	"github.com/sdcio/data-server/pkg/grpcserver"
	dsutils "github.com/sdcio/data-server/pkg/utils"
	logf "github.com/sdcio/logger"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // register auth plugins
	"k8s.io/component-base/logs"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

const defaultDataServerConfig = "/config/data-server.yaml"

var (
	schemaBaseDir = "/schemas"
	workspaceDir  = "/workspace"
)

var version = "dev"
var commit = ""

func main() {
	logs.InitLogs()
	defer logs.FlushLogs()

	verbosity.RegisterFlag()
	flag.Parse()

	logVerbosity := verbosity.FromArgs(os.Args[1:])
	level := verbosity.LogLevel(logVerbosity)

	l := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			return dsutils.RedactAttr(groups, logf.ReplaceTimeAttr(groups, a))
		},
	})).With("logger", "sdc-controller")
	slog.SetDefault(l)
	logger := logr.FromSlogHandler(l.Handler()).WithName("controller")
	logf.SetDefaultLogger(logger)
	ctx := logf.IntoContext(ctrl.SetupSignalHandler(), logger)
	log := logf.FromContext(ctx)

	// controller-runtime logs through logr and client-go through klog, so give
	// them the same handler.
	ctrl.SetLogger(logger)
	klog.SetSlogLogger(l)

	log.Info("controller bootstrap", "version", version, "commit", commit, "verbosity", logVerbosity)

	// setup controllers
	runScheme := runtime.NewScheme()
	// add the core object to the scheme
	for _, api := range (runtime.SchemeBuilder{
		clientgoscheme.AddToScheme,
		configv1alpha1.AddToScheme,
		invv1alpha1.AddToScheme,
	}) {
		if err := api(runScheme); err != nil {
			log.Error(err, "cannot add scheme")
			os.Exit(1)
		}
	}

	var tlsOpts []func(*tls.Config)
	metricsServerOptions := metricsserver.Options{
		BindAddress:   MetricBindAddress(),
		SecureServing: true,
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.1/pkg/metrics/filters#WithAuthenticat
		FilterProvider: filters.WithAuthenticationAndAuthorization,
		// If CertDir, CertName, and KeyName are not specified, controller-runtime will automatically
		// generate self-signed certificates for the metrics server. While convenient for development and testing,
		// this setup is not recommended for production.
		TLSOpts: tlsOpts,
	}

	mgr_options := ctrl.Options{
		Scheme:                 runScheme,
		Metrics:                metricsServerOptions,
		HealthProbeBindAddress: HealthProbeBindAddress(),
		Controller: config.Controller{
			MaxConcurrentReconciles: 16,
		},
	}
	if port := IsPProfEnabled(); port != nil {
		mgr_options.PprofBindAddress = *port
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), mgr_options)
	if err != nil {
		log.Error(err, "cannot start manager")
		os.Exit(1)
	}

	// SchemaServerBaseDir is overwritable via Environment var
	if envDir, found := os.LookupEnv("SDC_SCHEMA_SERVER_BASE_DIR"); found {
		schemaBaseDir = envDir
	}

	// WorkspaceBaseDir is overwritable via Environment var
	if envDir, found := os.LookupEnv("SDC_WORKSPACE_DIR"); found {
		workspaceDir = envDir
	}

	ctrlCfg := &ctrlconfig.ControllerConfig{
		SchemaDir:    schemaBaseDir,
		WorkspaceDir: workspaceDir,
	}

	var dsCore *core.Core
	if needsDataServer() {
		dsCfg, err := loadDataServerConfig()
		if err != nil {
			log.Error(err, "cannot load data-server config")
			os.Exit(1)
		}
		var dsGRPC *grpcserver.Server
		dsCore, dsGRPC, err = grpcserver.New(ctx, dsCfg)
		if err != nil {
			log.Error(err, "cannot create data-server")
			os.Exit(1)
		}
		if err := mgr.Add(&dataServerCore{core: dsCore}); err != nil {
			log.Error(err, "cannot add data-server core to manager")
			os.Exit(1)
		}
		if err := mgr.Add(&dataServerGRPC{srv: dsGRPC}); err != nil {
			log.Error(err, "cannot add data-server grpc server to manager")
			os.Exit(1)
		}

		tm := targetmanager.NewTargetManager(dsCore, mgr.GetClient())
		ctrlCfg.TargetManager = tm
		ctrlCfg.Transactions = dsCore

		promserver := prometheusserver.NewServer(&prometheusserver.Config{
			Address:       ":9443",
			TargetManager: tm,
		})
		go func() {
			if err := promserver.Start(ctx); err != nil {
				log.Error(err, "cannot start promerver")
				os.Exit(1)
			}
		}()
	}

	for name, reconciler := range reconcilers.Reconcilers {
		log.Info("reconciler", "name", name, "enabled", IsReconcilerEnabled(name))
		if IsReconcilerEnabled(name) {
			_, err := reconciler.SetupWithManager(ctx, mgr, ctrlCfg)
			if err != nil {
				log.Error(err, "cannot add controllers to manager")
				os.Exit(1)
			}
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", readyzCheck(dsCore)); err != nil {
		log.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	log.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		log.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func IsPProfEnabled() *string {
	if val, found := os.LookupEnv("PPROF_PORT"); found {
		port, err := strconv.Atoi(val)
		if err != nil {
			return nil
		}
		return ptr.To(fmt.Sprintf("127.0.0.1:%d", port))
	}
	return nil
}

// needsDataServer reports whether an enabled reconciler talks to the embedded
// data-server, so processes running only other reconcilers skip embedding it.
func needsDataServer() bool {
	for _, name := range []string{"targetdatastore", "targetconfig", "subscription", "rollout"} {
		if IsReconcilerEnabled(name) {
			return true
		}
	}
	return false
}

// IsReconcilerEnabled checks if an environment variable `ENABLE_<reconcilerName>` exists
// return "true" if the var is set and is not equal to "false".
func IsReconcilerEnabled(reconcilerName string) bool {
	if val, found := os.LookupEnv(fmt.Sprintf("ENABLE_%s", strings.ToUpper(reconcilerName))); found {
		if strings.ToLower(val) != "false" {
			return true
		}
	}
	return false
}

// HealthProbeBindAddress returns the healthz/readyz listen address; it must
// not clash with the metrics (8443) or pprof (8081) ports.
func HealthProbeBindAddress() string {
	if val, found := os.LookupEnv("HEALTH_PROBE_PORT"); found {
		return fmt.Sprintf(":%s", val)
	}
	return ":8090"
}

// loadDataServerConfig reads the embedded data-server config from
// DATA_SERVER_CONFIG (default /config/data-server.yaml) and applies the
// environment overrides and the merged-mode settings.
func loadDataServerConfig() (*dsconfig.Config, error) {
	path := defaultDataServerConfig
	if val, found := os.LookupEnv("DATA_SERVER_CONFIG"); found {
		path = val
	}
	cfg, err := dsconfig.New(path)
	if err != nil {
		return nil, err
	}
	if envDir, found := os.LookupEnv("SDC_SCHEMA_SERVER_BASE_DIR"); found &&
		cfg.GRPCServer != nil && cfg.GRPCServer.SchemaServer != nil {
		cfg.GRPCServer.SchemaServer.SchemasDirectory = envDir
	}
	// The embedded server must not open its own metrics listener
	// (prometheus.address); the process exposes metrics through the
	// controller-runtime and :9443 servers.
	cfg.Prometheus = nil
	return cfg, nil
}

// readyzCheck fails until the data-server core finished starting; without a
// core (no enabled reconciler uses one) it only checks liveness.
func readyzCheck(c *core.Core) healthz.Checker {
	if c == nil {
		return healthz.Ping
	}
	return func(*http.Request) error {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		if err := c.WaitReady(ctx); err != nil {
			return fmt.Errorf("data-server not ready: %w", err)
		}
		return nil
	}
}

// dataServerCore runs the data-server core under the manager: it starts the
// core, fails the manager if initialisation fails and stops the core on
// shutdown.
type dataServerCore struct {
	core *core.Core
}

func (d *dataServerCore) NeedLeaderElection() bool { return false }

func (d *dataServerCore) Start(ctx context.Context) error {
	if err := d.core.Start(ctx); err != nil {
		return fmt.Errorf("data-server core: %w", err)
	}
	<-ctx.Done()
	// the manager context is already cancelled, so stop on a fresh one
	stopCtx, cancel := context.WithTimeout(logf.IntoContext(context.Background(), logf.FromContext(ctx)), 30*time.Second)
	defer cancel()
	return d.core.Stop(stopCtx)
}

// dataServerGRPC runs the :56000 gRPC adapter under the manager and stops it
// on shutdown.
type dataServerGRPC struct {
	srv *grpcserver.Server
}

func (d *dataServerGRPC) NeedLeaderElection() bool { return false }

func (d *dataServerGRPC) Start(ctx context.Context) error {
	defer d.srv.Stop()
	if err := d.srv.Run(ctx); err != nil {
		return fmt.Errorf("data-server grpc: %w", err)
	}
	return nil
}

func MetricBindAddress() string {
	if val, found := os.LookupEnv("METRIC_PORT"); found {
		return fmt.Sprintf(":%s", val)
	}
	return ":8443"
}
