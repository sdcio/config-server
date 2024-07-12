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
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/henderiw/apiserver-builder/pkg/builder"
	"github.com/henderiw/apiserver-store/pkg/db/badgerdb"
	"github.com/henderiw/apiserver-store/pkg/storebackend"
	memstore "github.com/henderiw/apiserver-store/pkg/storebackend/memory"
	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/apis/config"
	"github.com/sdcio/config-server/apis/config/handlers"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	_ "github.com/sdcio/config-server/pkg/discovery/discoverers/all"
	configopenapi "github.com/sdcio/config-server/pkg/generated/openapi"
	"github.com/sdcio/config-server/pkg/reconcilers"
	_ "github.com/sdcio/config-server/pkg/reconcilers/all"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	genericregistry "github.com/sdcio/config-server/pkg/registry/generic"
	"github.com/sdcio/config-server/pkg/registry/options"
	runningconfigregistry "github.com/sdcio/config-server/pkg/registry/runningconfig"
	sdcctx "github.com/sdcio/config-server/pkg/sdc/ctx"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	ssclient "github.com/sdcio/config-server/pkg/sdc/schemaserver/client"
	"github.com/sdcio/config-server/pkg/target"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // register auth plugins
	"k8s.io/component-base/logs"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	localDataServerAddress = "localhost:56000"
	defaultEtcdPathPrefix  = "/registry/config.sdcio.dev"
)

var (
	schemaBaseDir = "/schemas"
	configDir     = "/config"
)

func main() {
	logs.InitLogs()
	defer logs.FlushLogs()

	l := log.NewLogger(&log.HandlerOptions{Name: "config-server-logger", AddSource: false})
	slog.SetDefault(l)
	ctx := log.IntoContext(context.Background(), l)
	log := log.FromContext(ctx)

	opts := zap.Options{
		TimeEncoder: zapcore.RFC3339NanoTimeEncoder,
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	targetStore := memstore.NewStore[*target.Context]()
	// TODO dataServer/schemaServer -> this should be decoupled in a scaled out environment
	time.Sleep(5 * time.Second)
	dataServerStore := memstore.NewStore[sdcctx.DSContext]()
	if err := createDataServerClient(ctx, dataServerStore); err != nil {
		log.Error("cannot create data server", "error", err.Error())
		os.Exit(1)
	}
	schemaServerStore := memstore.NewStore[sdcctx.SSContext]()
	if err := createSchemaServerClient(ctx, schemaServerStore); err != nil {
		log.Error("cannot create schema server", "error", err.Error())
		os.Exit(1)
	}

	// setup controllers
	runScheme := runtime.NewScheme()
	/*
		if err := scheme.AddToScheme(runScheme); err != nil {
			log.Error("cannot initialize schema", "error", err)
			os.Exit(1)
		}
	*/
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

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: runScheme,
	})
	if err != nil {
		log.Error("cannot start manager", "err", err)
		os.Exit(1)
	}

	// SchemaServerBaseDir is overwritable via Environment var
	if envDir, found := os.LookupEnv("SDC_SCHEMA_SERVER_BASE_DIR"); found {
		schemaBaseDir = envDir
	}

	// SchemaServerBaseDir is overwritable via Environment var
	if envDir, found := os.LookupEnv("SDC_CONFIG_DIR"); found {
		configDir = envDir
	}

	targetHandler := target.NewTargetHandler(mgr.GetClient(), targetStore)

	ctrlCfg := &ctrlconfig.ControllerConfig{
		TargetStore:       targetStore,
		DataServerStore:   dataServerStore,
		SchemaServerStore: schemaServerStore,
		SchemaDir:         schemaBaseDir,
		TargetHandler: targetHandler,
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

	db, err := badgerdb.OpenDB(ctx, configDir)
	if err != nil {
		log.Error("cannot open db", "err", err.Error())
		os.Exit(1)
	}

	registryOptions := &options.Options{
		Prefix: configDir,
		Type:   options.StorageType_KV,
		DB:     db,
	}

	configHandler := handlers.ConfigStoreHandler{Handler: targetHandler}

	configregistryOptions := *registryOptions
	configregistryOptions.DryRunCreateFn = configHandler.DryRunCreateFn
	configregistryOptions.DryRunUpdateFn = configHandler.DryRunUpdateFn
	configregistryOptions.DryRunDeleteFn = configHandler.DryRunDeleteFn

	configStorageProvider := genericregistry.NewStorageProvider(ctx, &config.Config{}, &configregistryOptions)
	configSetStorageProvider := genericregistry.NewStorageProvider(ctx, &config.ConfigSet{}, registryOptions)
	unmanagedConfigStorageProvider := genericregistry.NewStorageProvider(ctx, &config.UnManagedConfig{}, registryOptions)
	// no storage required since the targetStore is acting as the storage for the running config resource
	runningConfigStorageProvider := runningconfigregistry.NewStorageProvider(ctx, &config.RunningConfig{}, &options.Options{
		Client:      mgr.GetClient(),
		TargetStore: targetStore,
	})

	go func() {
		if err := builder.APIServer.
			WithServerName("config-server").
			WithOpenAPIDefinitions("Config", "v1alpha1", configopenapi.GetOpenAPIDefinitions).
			WithResourceAndHandler(&config.Config{}, configStorageProvider).
			WithResourceAndHandler(&configv1alpha1.Config{}, configStorageProvider).
			WithResourceAndHandler(&config.ConfigSet{}, configSetStorageProvider).
			WithResourceAndHandler(&configv1alpha1.ConfigSet{}, configSetStorageProvider).
			WithResourceAndHandler(&config.UnManagedConfig{}, unmanagedConfigStorageProvider).
			WithResourceAndHandler(&configv1alpha1.UnManagedConfig{}, unmanagedConfigStorageProvider).
			WithResourceAndHandler(&config.RunningConfig{}, runningConfigStorageProvider).
			WithResourceAndHandler(&configv1alpha1.RunningConfig{}, runningConfigStorageProvider).
			WithoutEtcd().
			Execute(ctx); err != nil {
			log.Info("cannot start config-server")
		}
	}()

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

func createDataServerClient(ctx context.Context, dataServerStore storebackend.Storer[sdcctx.DSContext]) error {
	log := log.FromContext(ctx)

	dataServerAddress := localDataServerAddress
	if address, found := os.LookupEnv("SDC_DATA_SERVER"); found {
		dataServerAddress = address
	}

	// TODO the population of the dataservers in the store should become dynamic, through a controller
	// right now it is static since all of this happens in the same pod and we dont have scaled out dataservers
	dsConfig := &dsclient.Config{
		Address: dataServerAddress,
	}
	dsClient, err := dsclient.New(dsConfig)
	if err != nil {
		log.Error("cannot initialize dataserver client", "err", err)
		return err
	}
	if err := dsClient.Start(ctx); err != nil {
		log.Error("cannot start dataserver client", "err", err)
		return err
	}
	dsCtx := sdcctx.DSContext{
		Config:   dsConfig,
		Targets:  sets.New[string](),
		DSClient: dsClient,
	}
	if err := dataServerStore.Create(ctx, storebackend.ToKey(dataServerAddress), dsCtx); err != nil {
		log.Error("cannot store datastore context in dataserver", "err", err)
		return err
	}
	log.Info("dataserver client created")
	return nil
}

func createSchemaServerClient(ctx context.Context, schemaServerStore storebackend.Storer[sdcctx.SSContext]) error {
	log := log.FromContext(ctx)

	// For the schema server we first check if the SDC_SCHEMA_SERVER was et if not we could also use
	// the SDC_DATA_SERVER as fallback. If none are set it is the default address (localhost)
	schemaServerAddress := localDataServerAddress
	if address, found := os.LookupEnv("SDC_SCHEMA_SERVER"); found {
		schemaServerAddress = address
	} else {
		if address, found := os.LookupEnv("SDC_DATA_SERVER"); found {
			schemaServerAddress = address
		}
	}

	ssConfig := &ssclient.Config{
		Address: schemaServerAddress,
	}
	ssClient, err := ssclient.New(ssConfig)
	if err != nil {
		log.Error("cannot initialize schemaserver client", "err", err)
		return err
	}
	if err := ssClient.Start(ctx); err != nil {
		log.Error("cannot start schemaserver client", "err", err)
		return err
	}
	ssCtx := sdcctx.SSContext{
		Config:   ssConfig,
		SSClient: ssClient,
	}
	if err := schemaServerStore.Create(ctx, storebackend.ToKey(schemaServerAddress), ssCtx); err != nil {
		log.Error("cannot store schema context in schemaserver", "err", err)
		return err
	}
	log.Info("schemaserver client created")
	return nil
}
