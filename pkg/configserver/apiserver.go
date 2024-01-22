package configserver

import (
	"context"
	"fmt"

	configv1alpha1 "github.com/iptecharch/config-server/apis/config/v1alpha1"
	"github.com/iptecharch/config-server/pkg/store"
	"github.com/iptecharch/config-server/pkg/target"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	// Scheme defines methods for serializing and deserializing API objects.
	Scheme = runtime.NewScheme()
	// Codecs provides methods for retrieving codecs and serializers for specific
	// versions and content types.
	Codecs = serializer.NewCodecFactory(Scheme)
)

func init() {
	utilruntime.Must(configv1alpha1.AddToScheme(Scheme))

	// we need to add the options to empty v1
	// TODO fix the server code to avoid this
	metav1.AddToGroupVersion(Scheme, schema.GroupVersion{Version: "v1"})

	// TODO: keep the generic API server from wanting this
	unversioned := schema.GroupVersion{Group: "", Version: "v1"}
	Scheme.AddUnversionedTypes(unversioned,
		&metav1.Status{},
		&metav1.APIVersions{},
		&metav1.APIGroupList{},
		&metav1.APIGroup{},
		&metav1.APIResourceList{},
	)
}

// ExtraConfig holds custom apiserver config
type ExtraConfig struct {
}

// Config defines the config for the apiserver
type Config struct {
	GenericConfig *genericapiserver.RecommendedConfig
	ExtraConfig   ExtraConfig
}

// ConfigServer contains state for a Kubernetes cluster master/api server.
type ConfigServer struct {
	GenericAPIServer *genericapiserver.GenericAPIServer
}

type completedConfig struct {
	GenericConfig genericapiserver.CompletedConfig
	ExtraConfig   *ExtraConfig
}

// CompletedConfig embeds a private pointer that cannot be instantiated outside of this package.
type CompletedConfig struct {
	*completedConfig
}

// Complete fills in any fields not set that are required to have valid data. It's mutating the receiver.
func (cfg *Config) Complete() CompletedConfig {
	c := completedConfig{
		cfg.GenericConfig.Complete(),
		&cfg.ExtraConfig,
	}

	c.GenericConfig.Version = &version.Info{
		Major: "1",
		Minor: "0",
	}

	return CompletedConfig{&c}
}

// New returns a new instance of ConfigServer from the given config.
func (c completedConfig) New(ctx context.Context, client client.Client, targetStore store.Storer[target.Context]) (*ConfigServer, error) {
	genericServer, err := c.GenericConfig.New("config-server", genericapiserver.NewEmptyDelegate())
	if err != nil {
		return nil, err
	}

	configGroup, err := NewRESTStorage(ctx, Scheme, Codecs, client, targetStore)
	if err != nil {
		return nil, err
	}

	s := &ConfigServer{
		GenericAPIServer: genericServer,
	}

	// Install the groups.
	if err := s.GenericAPIServer.InstallAPIGroups(&configGroup); err != nil {
		return nil, err
	}

	return s, nil
}

func NewRESTStorage(ctx context.Context, scheme *runtime.Scheme, codecs serializer.CodecFactory, client client.Client, targetStore store.Storer[target.Context]) (genericapiserver.APIGroupInfo, error) {
	config, err := NewConfigFileProvider(ctx, &configv1alpha1.Config{}, scheme, client, targetStore)
	if err != nil {
		return genericapiserver.APIGroupInfo{}, err
	}
	configset, err := NewConfigSetFileProvider(ctx, &configv1alpha1.ConfigSet{}, scheme, client, config.GetStore(), targetStore)
	if err != nil {
		return genericapiserver.APIGroupInfo{}, err
	}

	group := genericapiserver.NewDefaultAPIGroupInfo(configv1alpha1.SchemeGroupVersion.Group, scheme, metav1.ParameterCodec, codecs)

	group.VersionedResourcesStorageMap = map[string]map[string]rest.Storage{
		configv1alpha1.SchemeGroupVersion.Version: {
			"configs":    config,
			"configsets": configset,
		},
	}

	{
		gvk := schema.GroupVersionKind{
			Group:   configv1alpha1.SchemeGroupVersion.Group,
			Version: configv1alpha1.SchemeGroupVersion.Version,
			Kind:    configv1alpha1.ConfigKind,
		}

		scheme.AddFieldLabelConversionFunc(gvk, convertConfigFieldSelector)
	}
	{
		gvk := schema.GroupVersionKind{
			Group:   configv1alpha1.SchemeGroupVersion.Group,
			Version: configv1alpha1.SchemeGroupVersion.Version,
			Kind:    configv1alpha1.ConfigSetKind,
		}

		scheme.AddFieldLabelConversionFunc(gvk, convertConfigSetFieldSelector)
	}

	return group, nil
}

// convertConfigFieldSelector is the schema conversion function for normalizing the the FieldSelector for Config
func convertConfigFieldSelector(label, value string) (internalLabel, internalValue string, err error) {
	switch label {
	case "metadata.name":
		return label, value, nil
	case "metadata.namespace":
		return label, value, nil
	default:
		return "", "", fmt.Errorf("%q is not a known field selector", label)
	}
}

// convertConfigSetFieldSelector is the schema conversion function for normalizing the the FieldSelector for ConfigSet
func convertConfigSetFieldSelector(label, value string) (internalLabel, internalValue string, err error) {
	switch label {
	case "metadata.name":
		return label, value, nil
	case "metadata.namespace":
		return label, value, nil
	default:
		return "", "", fmt.Errorf("%q is not a known field selector", label)
	}
}
