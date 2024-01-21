/*
Copyright 2017 The Kubernetes Authors.

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
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	"github.com/henderiw/apiserver-builder/pkg/builder"
	"github.com/henderiw/logger/log"
	configv1alpha1 "github.com/iptecharch/config-server/apis/config/v1alpha1"
	clientset "github.com/iptecharch/config-server/apis/generated/clientset/versioned"
	"github.com/iptecharch/config-server/apis/generated/clientset/versioned/scheme"
	informers "github.com/iptecharch/config-server/apis/generated/informers/externalversions"
	configopenapi "github.com/iptecharch/config-server/apis/generated/openapi"
	"github.com/iptecharch/config-server/pkg/configserver"
	_ "github.com/iptecharch/config-server/pkg/discovery/discoverers/all"
	"github.com/iptecharch/config-server/pkg/reconcilers"
	_ "github.com/iptecharch/config-server/pkg/reconcilers/all"
	"github.com/iptecharch/config-server/pkg/reconcilers/ctrlconfig"
	sdcctx "github.com/iptecharch/config-server/pkg/sdc/ctx"
	dsclient "github.com/iptecharch/config-server/pkg/sdc/dataserver/client"
	ssclient "github.com/iptecharch/config-server/pkg/sdc/schemaserver/client"
	"github.com/iptecharch/config-server/pkg/store"
	memstore "github.com/iptecharch/config-server/pkg/store/memory"
	"github.com/iptecharch/config-server/pkg/target"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.uber.org/zap/zapcore"
	"k8s.io/apiextensions-apiserver/pkg/apiserver"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/endpoints/openapi"
	genericapiserver "k8s.io/apiserver/pkg/server"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // register auth plugins
	"k8s.io/component-base/logs"
	netutils "k8s.io/utils/net"
	ctrl "sigs.k8s.io/controller-runtime"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	localDataServerAddress = "localhost:56000"
	defaultEtcdPathPrefix  = "/registry/config.sdcio.dev"
)

var (
	schemaBaseDir = "/schemas"
)

func main() {
	logs.InitLogs()
	defer logs.FlushLogs()

	l := log.NewLogger(&log.HandlerOptions{Name: "config-server-logger", AddSource: false})
	slog.SetDefault(l)
	ctx := log.IntoContext(context.Background(), l)
	log := log.FromContext(ctx)

	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	//opts.BindFlags(flag.CommandLine)
	//flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	//configStore := memstore.NewStore[runtime.Object]()

	targetStore := memstore.NewStore[target.Context]()
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
	if err := scheme.AddToScheme(runScheme); err != nil {
		log.Error("cannot initialize schema", "error", err)
		os.Exit(1)
	}
	// add the core object to the scheme
	for _, api := range (runtime.SchemeBuilder{
		clientgoscheme.AddToScheme,
		configv1alpha1.AddToScheme,
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

	configProvider, err := configserver.NewConfigFileProvider(ctx, &configv1alpha1.Config{}, runScheme, mgr.GetClient(), targetStore)
	if err != nil {
		log.Error("cannot create config rest storage", "err", err)
		os.Exit(1)
	}
	configSetProvider, err := configserver.NewConfigSetFileProvider(ctx, &configv1alpha1.ConfigSet{}, runScheme, mgr.GetClient(), configProvider.GetStore())
	if err != nil {
		log.Error("cannot create config rest storage", "err", err)
		os.Exit(1)
	}

	ctrlCfg := &ctrlconfig.ControllerConfig{
		//ConfigStore:     configStore,
		TargetStore:       targetStore,
		DataServerStore:   dataServerStore,
		SchemaServerStore: schemaServerStore,
		SchemaDir:         schemaBaseDir,
		ConfigProvider:    configProvider,
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

	go func() {
		if err := builder.APIServer.
			WithServerName("config-server").
			WithEtcdPath(defaultEtcdPathPrefix).
			WithOpenAPIDefinitions("Config", "v0.0.0", configopenapi.GetOpenAPIDefinitions).
			WithResourceAndHandler(ctx, &configv1alpha1.Config{}, configserver.NewConfigProviderHandler(ctx, configProvider)).
			WithResourceAndHandler(ctx, &configv1alpha1.ConfigSet{}, configserver.NewConfigSetProviderHandler(ctx, configSetProvider)).
			WithoutEtcd().
			Execute(ctx); err != nil {
			log.Info("cannot start caas")
		}

		/*
			if err := builder.APIServer.
				WithOpenAPIDefinitions("Config", "v0.0.0", openapi.GetOpenAPIDefinitions).
				//WithResourceAndHandler(&v1alpha1.Config{}, filepath.NewJSONFilepathStorageProvider(&v1alpha1.Config{}, "data")).
				WithResourceAndHandler(&v1alpha1.Config{}, configserver.NewConfigProvider(ctx, &v1alpha1.Config{}, configCtx)).
				WithResourceAndHandler(&v1alpha1.ConfigSet{}, configserver.NewConfigSetProvider(ctx, &v1alpha1.ConfigSet{}, configCtx)).
				WithoutEtcd().
				WithLocalDebugExtension().
				Execute(); err != nil {
				log.Info("cannot start caas")
			}
		*/
		/*
			options := NewConfigServerOptions(os.Stdout, os.Stderr, mgr.GetClient(), targetStore)
			cmd := NewCommandStartConfigServer(ctx, options)
			cmd.Execute()
		*/
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

func createDataServerClient(ctx context.Context, dataServerStore store.Storer[sdcctx.DSContext]) error {
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
	if err := dataServerStore.Create(ctx, store.ToKey(dataServerAddress), dsCtx); err != nil {
		log.Error("cannot store datastore context in dataserver", "err", err)
		return err
	}
	log.Info("dataserver client created")
	return nil
}

func createSchemaServerClient(ctx context.Context, schemaServerStore store.Storer[sdcctx.SSContext]) error {
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
	if err := schemaServerStore.Create(ctx, store.ToKey(schemaServerAddress), ssCtx); err != nil {
		log.Error("cannot store schema context in schemaserver", "err", err)
		return err
	}
	log.Info("schemaserver client created")
	return nil
}

type ConfigServerOptions struct {
	RecommendedOptions       *genericoptions.RecommendedOptions
	LocalStandaloneDebugging bool // Enables local standalone running/debugging of the apiserver.
	Client                   client.Client
	TargetStore              store.Storer[target.Context]
	SharedInformerFactory    informers.SharedInformerFactory

	StdOut io.Writer
	StdErr io.Writer
}

func NewConfigServerOptions(out, errOut io.Writer, client client.Client, targetStore store.Storer[target.Context]) *ConfigServerOptions {
	versions := schema.GroupVersions{
		configv1alpha1.SchemeGroupVersion,
	}

	o := &ConfigServerOptions{
		RecommendedOptions: genericoptions.NewRecommendedOptions(
			defaultEtcdPathPrefix,
			apiserver.Codecs.LegacyCodec(versions...),
		),
		Client:      client,
		TargetStore: targetStore,
		StdOut:      out,
		StdErr:      errOut,
	}
	o.RecommendedOptions.Etcd.StorageConfig.EncodeVersioner = versions
	o.RecommendedOptions.Etcd = nil
	return o
}

// NewCommandStartConfigServer provides a CLI handler for 'start master' command
// with a default ConfigServerOptions.
func NewCommandStartConfigServer(ctx context.Context, defaults *ConfigServerOptions) *cobra.Command {
	o := *defaults
	cmd := &cobra.Command{
		Short: "launch a config-server",
		Long:  "Launch a config-server",
		RunE: func(c *cobra.Command, args []string) error {
			if err := o.Complete(); err != nil {
				return err
			}
			if err := o.Validate(args); err != nil {
				return err
			}
			if err := o.RunConfigServer(ctx); err != nil {
				return err
			}
			return nil
		},
	}

	flags := cmd.Flags()
	o.AddFlags(flags)

	return cmd
}

// Complete fills in fields required to have valid data
func (o *ConfigServerOptions) Complete() error {
	if o.LocalStandaloneDebugging {
		o.RecommendedOptions.Authorization = nil
		o.RecommendedOptions.CoreAPI = nil
		o.RecommendedOptions.Admission = nil
		o.RecommendedOptions.Authentication.RemoteKubeConfigFileOptional = true
	}

	// if !o.LocalStandaloneDebugging {
	// 	TODO: register admission plugins here ...
	// 	add admission plugins to the RecommendedPluginOrder here ...
	// }

	return nil
}

// Validate validates ConfigServerOptions
func (o ConfigServerOptions) Validate(args []string) error {
	errors := []error{}
	errors = append(errors, o.RecommendedOptions.Validate()...)
	return utilerrors.NewAggregate(errors)
}

func (o ConfigServerOptions) RunConfigServer(ctx context.Context) error {
	config, err := o.Config()
	if err != nil {
		return err
	}

	server, err := config.Complete().New(ctx, o.Client, o.TargetStore)
	if err != nil {
		return err
	}
	if config.GenericConfig.SharedInformerFactory != nil {
		server.GenericAPIServer.AddPostStartHookOrDie("start-config-server-informers", func(context genericapiserver.PostStartHookContext) error {
			config.GenericConfig.SharedInformerFactory.Start(context.StopCh)
			o.SharedInformerFactory.Start(context.StopCh)
			return nil
		})
	}
	return server.GenericAPIServer.PrepareRun().Run(ctx.Done())
}

// Config returns config for the api server given ConfigServerOptions
func (o *ConfigServerOptions) Config() (*configserver.Config, error) {
	// TODO have a "real" external address
	if err := o.RecommendedOptions.SecureServing.MaybeDefaultWithSelfSignedCerts("localhost", nil, []net.IP{netutils.ParseIPSloppy("127.0.0.1")}); err != nil {
		return nil, fmt.Errorf("error creating self-signed certificates: %w", err)
	}

	//if o.RecommendedOptions.Etcd != nil {
	//	o.RecommendedOptions.Etcd.StorageConfig.Paging = utilfeature.DefaultFeatureGate.Enabled(features.APIListChunking)
	//}

	o.RecommendedOptions.ExtraAdmissionInitializers = func(c *genericapiserver.RecommendedConfig) ([]admission.PluginInitializer, error) {
		client, err := clientset.NewForConfig(c.LoopbackClientConfig)
		if err != nil {
			return nil, err
		}
		informerFactory := informers.NewSharedInformerFactory(client, c.LoopbackClientConfig.Timeout)
		o.SharedInformerFactory = informerFactory
		return []admission.PluginInitializer{}, nil
	}

	serverConfig := genericapiserver.NewRecommendedConfig(apiserver.Codecs)

	serverConfig.OpenAPIConfig = genericapiserver.DefaultOpenAPIConfig(configopenapi.GetOpenAPIDefinitions, openapi.NewDefinitionNamer(apiserver.Scheme))
	serverConfig.OpenAPIConfig.Info.Title = "Config"
	serverConfig.OpenAPIConfig.Info.Version = "0.1"

	serverConfig.OpenAPIV3Config = genericapiserver.DefaultOpenAPIV3Config(configopenapi.GetOpenAPIDefinitions, openapi.NewDefinitionNamer(apiserver.Scheme))
	serverConfig.OpenAPIV3Config.Info.Title = "Config"
	serverConfig.OpenAPIV3Config.Info.Version = "0.1"

	if err := o.RecommendedOptions.ApplyTo(serverConfig); err != nil {
		return nil, err
	}

	config := &configserver.Config{
		GenericConfig: serverConfig,
	}
	return config, nil
}

func (o *ConfigServerOptions) AddFlags(fs *pflag.FlagSet) {
	// Add base flags
	o.RecommendedOptions.AddFlags(fs)
	utilfeature.DefaultMutableFeatureGate.AddFlag(fs)

	// Add additional flags.
}
