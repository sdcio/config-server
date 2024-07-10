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

package runningconfig
/*
import (
	"context"

	builderrest "github.com/henderiw/apiserver-builder/pkg/builder/rest"
	"github.com/henderiw/apiserver-store/pkg/generic/registry"
	"github.com/henderiw/apiserver-store/pkg/storebackend"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/sdcio/config-server/pkg/target"
	"go.opentelemetry.io/otel"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewProvider(ctx context.Context, client client.Client, targetStore storebackend.Storer[*target.Context]) builderrest.ResourceHandlerProvider {
	return func(ctx context.Context, scheme *runtime.Scheme, getter generic.RESTOptionsGetter) (rest.Storage, error) {
		return NewREST(ctx, scheme, getter, client, targetStore)
	}
}

// NewPackageRevisionREST returns a RESTStorage object that will work against API services.
func NewREST(ctx context.Context, scheme *runtime.Scheme, optsGetter generic.RESTOptionsGetter, client client.Client, targetStore storebackend.Storer[*target.Context]) (rest.Storage, error) {
	scheme.AddFieldLabelConversionFunc(
		schema.GroupVersionKind{
			Group:   configv1alpha1.Group,
			Version: configv1alpha1.Version,
			Kind:    configv1alpha1.RunningConfigKind,
		},
		configv1alpha1.ConvertRunningConfigFieldSelector,
	)

	gr := configv1alpha1.Resource(configv1alpha1.RunningConfigPlural)
	strategy := NewStrategy(ctx, scheme, client, targetStore)

	// This is the etcd store
	store := &registry.Store{
		Tracer:                    otel.Tracer("config-server"),
		NewFunc:                   func() runtime.Object { return &configv1alpha1.RunningConfig{} },
		NewListFunc:               func() runtime.Object { return &configv1alpha1.RunningConfigList{} },
		PredicateFunc:             Match,
		DefaultQualifiedResource:  gr,
		SingularQualifiedResource: configv1alpha1.Resource(configv1alpha1.RunningConfigSingular),
		GetStrategy:               strategy,
		ListStrategy:              strategy,
		CreateStrategy:            strategy,
		UpdateStrategy:            strategy,
		DeleteStrategy:            strategy,
		WatchStrategy:             strategy,

		TableConvertor: NewTableConvertor(configv1alpha1.Resource(configv1alpha1.RunningConfigPlural)),
	}
	options := &generic.StoreOptions{
		RESTOptions: optsGetter,
		AttrFunc:    GetAttrs,
	}
	if err := store.CompleteWithOptions(options); err != nil {
		return nil, err
	}
	return store, nil
}
*/