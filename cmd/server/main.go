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

//go:generate apiserver-runtime-gen
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/henderiw/apiserver-builder/pkg/builder"
	"github.com/henderiw/apiserver-store/pkg/db/badgerdb"
	"github.com/henderiw/logger/log"
	sdcconfig "github.com/sdcio/config-server/apis/config"
	"github.com/sdcio/config-server/apis/config/handlers"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/generated/openapi"
	_ "github.com/sdcio/config-server/pkg/reconcilers/all"
	configblameregistry "github.com/sdcio/config-server/pkg/registry/configblame"
	genericregistry "github.com/sdcio/config-server/pkg/registry/generic"
	"github.com/sdcio/config-server/pkg/registry/options"
	runningconfigregistry "github.com/sdcio/config-server/pkg/registry/runningconfig"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // register auth plugins
	"k8s.io/component-base/logs"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	configDir     = "/config"
)

func main() {
	logs.InitLogs()
	defer logs.FlushLogs()

	l := log.NewLogger(&log.HandlerOptions{Name: "sdc-api-server-logger", AddSource: false})
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
	

	// ConfigServerBaseDir is overwritable via Environment var
	if envDir, found := os.LookupEnv("SDC_CONFIG_DIR"); found {
		configDir = envDir
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

	configHandler := handlers.ConfigStoreHandler{}

	configregistryOptions := *registryOptions
	configregistryOptions.DryRunCreateFn = configHandler.DryRunCreateFn
	configregistryOptions.DryRunUpdateFn = configHandler.DryRunUpdateFn
	configregistryOptions.DryRunDeleteFn = configHandler.DryRunDeleteFn

	configStorageProvider := genericregistry.NewStorageProvider(ctx, sdcconfig.BuildEmptyConfig(), &configregistryOptions)

	sensitiveconfigStorageProvider := genericregistry.NewStorageProvider(ctx, sdcconfig.BuildEmptySensitiveConfig(), &configregistryOptions)

	configSetStorageProvider := genericregistry.NewStorageProvider(ctx, sdcconfig.BuildEmptyConfigSet(), registryOptions)
	deviationStorageProvider := genericregistry.NewStorageProvider(ctx, sdcconfig.BuildEmptyDeviation(), registryOptions)
	// no storage required since the targetStore is acting as the storage for the running config resource
	runningConfigStorageProvider := runningconfigregistry.NewStorageProvider(ctx, sdcconfig.BuildEmptyRunningConfig(), &options.Options{
		//Client:      mgr.GetClient(),
		//TargetStore: targetStore,
	})
	// no storage required since the targetStore is acting as the storage for the running config resource
	configBlameStorageProvider := configblameregistry.NewStorageProvider(ctx, sdcconfig.BuildEmptyConfigBlame(), &options.Options{
		//Client:      mgr.GetClient(),
		//TargetStore: targetStore,
	})
	
	if err := builder.APIServer.
		WithServerName("sdc-apiserver").
		WithOpenAPIDefinitions("Config", "v1alpha1", openapi.GetOpenAPIDefinitions).
		WithResourceAndHandler(&sdcconfig.Config{}, configStorageProvider).
		WithResourceAndHandler(&configv1alpha1.Config{}, configStorageProvider).
		WithResourceAndHandler(&sdcconfig.SensitiveConfig{}, sensitiveconfigStorageProvider).
		WithResourceAndHandler(&configv1alpha1.SensitiveConfig{}, sensitiveconfigStorageProvider).
		WithResourceAndHandler(&sdcconfig.ConfigSet{}, configSetStorageProvider).
		WithResourceAndHandler(&configv1alpha1.ConfigSet{}, configSetStorageProvider).
		WithResourceAndHandler(&sdcconfig.Deviation{}, deviationStorageProvider).
		WithResourceAndHandler(&configv1alpha1.Deviation{}, deviationStorageProvider).
		WithResourceAndHandler(&sdcconfig.RunningConfig{}, runningConfigStorageProvider).
		WithResourceAndHandler(&configv1alpha1.RunningConfig{}, runningConfigStorageProvider).
		WithResourceAndHandler(&sdcconfig.ConfigBlame{}, configBlameStorageProvider).
		WithResourceAndHandler(&configv1alpha1.ConfigBlame{}, configBlameStorageProvider).
		WithoutEtcd().
		Execute(ctx); err != nil {
		log.Info("cannot start sdc api-server")
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
