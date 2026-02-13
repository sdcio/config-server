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
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	//"time"

	"github.com/henderiw/logger/log"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/output/prometheusserver"
	"github.com/sdcio/config-server/pkg/reconcilers"
	_ "github.com/sdcio/config-server/pkg/reconcilers/all"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	dsmanager "github.com/sdcio/config-server/pkg/sdc/dataserver/manager"
	targetmanager "github.com/sdcio/config-server/pkg/sdc/target/manager"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // register auth plugins
	"k8s.io/component-base/logs"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var (
	schemaBaseDir = "/schemas"
	workspaceDir  = "/workspace"
)

func main() {
	logs.InitLogs()
	defer logs.FlushLogs()

	l := log.NewLogger(&log.HandlerOptions{Name: "sdc-controller-logger", AddSource: false})
	slog.SetDefault(l)
	ctx := log.IntoContext(context.Background(), l)
	log := log.FromContext(ctx)

	opts := zap.Options{
		TimeEncoder: zapcore.RFC3339NanoTimeEncoder,
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// setup controllers
	runScheme := runtime.NewScheme()
	// add the core object to the scheme
	for _, api := range (runtime.SchemeBuilder{
		clientgoscheme.AddToScheme,
		configv1alpha1.AddToScheme,
		invv1alpha1.AddToScheme,
	}) {
		if err := api(runScheme); err != nil {
			log.Error("cannot add scheme", "err", err)
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
		Scheme:  runScheme,
		Metrics: metricsServerOptions,
		Controller: config.Controller{
			MaxConcurrentReconciles: 16,
		},
	}
	if port := IsPProfEnabled(); port != nil {
		mgr_options.PprofBindAddress = *port
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), mgr_options)
	if err != nil {
		log.Error("cannot start manager", "err", err)
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
	if IsLocalDataServerEnabled() {
		// Create the DS connection manager and register it with controller-runtime.
		dsCfg := &dsclient.Config{
			Address: dsclient.GetDataServerAddress(),
			// Insecure/TLS settings etc if needed
		}
		dsConnMgr := dsmanager.New(ctx, dsCfg)
		if err := dsConnMgr.AddToManager(mgr); err != nil {
			log.Error("cannot add dataserver conn manager", "err", err)
			os.Exit(1)
		}
		ctrlCfg.DataServerManager = dsConnMgr
		// mgr.Add -> calls dsConnMgr.Start(ctx)

		tm := targetmanager.NewTargetManager(dsConnMgr, mgr.GetClient())
		ctrlCfg.TargetManager = tm

		promserver := prometheusserver.NewServer(&prometheusserver.Config{
			Address:       ":9443",
			TargetManager: tm,
		})
		go func() {
			if err := promserver.Start(ctx); err != nil {
				log.Error("cannot start promerver", "err", err.Error())
				os.Exit(1)
			}
		}()
	}

	for name, reconciler := range reconcilers.Reconcilers {
		log.Info("reconciler", "name", name, "enabled", IsReconcilerEnabled(name))
		if IsReconcilerEnabled(name) {
			_, err := reconciler.SetupWithManager(ctx, mgr, ctrlCfg)
			if err != nil {
				log.Error("cannot add controllers to manager", "err", err.Error())
				os.Exit(1)
			}
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Error("unable to set up health check", "error", err.Error())
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.Error("unable to set up ready check", "error", err.Error())
		os.Exit(1)
	}

	log.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		log.Error("problem running manager", "error", err.Error())
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

func IsLocalDataServerEnabled() bool {
	if val, found := os.LookupEnv("LOCAL_DATASERVER"); found {
		if strings.ToLower(val) == "true" {
			return true
		}
	}
	return false
}

func MetricBindAddress() string {
	if val, found := os.LookupEnv("METRIC_PORT"); found {
		return fmt.Sprintf(":%s", val)
	}
	return ":8443"
}
