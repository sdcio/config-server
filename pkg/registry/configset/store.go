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

package configset

import (
	"context"
	"fmt"

	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	builderrest "github.com/henderiw/apiserver-builder/pkg/builder/rest"
	"github.com/henderiw/apiserver-store/pkg/generic/registry"
	"github.com/henderiw/apiserver-store/pkg/storebackend"
	watchermanager "github.com/henderiw/apiserver-store/pkg/watcher-manager"
	"github.com/sdcio/config-server/pkg/registry/options"
	"github.com/sdcio/config-server/pkg/registry/store"
	"go.opentelemetry.io/otel"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/rest"
)

func NewStorageProvider(ctx context.Context, obj resource.Object, opts *options.Options) builderrest.StorageProvider {
	watcherManager := watchermanager.New(64)

	go watcherManager.Start(ctx)

	sp := builderrest.StorageProvider{
		ResourceStorageProviderFn: func(scheme *runtime.Scheme, optsGetter generic.RESTOptionsGetter) (rest.Storage, error) {
			return NewREST(obj, scheme, watcherManager, optsGetter, opts)
		},
	}
	if _, ok := obj.(resource.ObjectWithStatusSubResource); ok {
		sp.StatusSubResourceStorageProviderFn = func(scheme *runtime.Scheme, store rest.Storage) (rest.Storage, error) {
			return NewStatusREST(obj, scheme, watcherManager, opts, store)
		}
	}
	// Add addtional subresources
	return sp
}

func NewREST(
	obj resource.Object,
	scheme *runtime.Scheme,
	watcherManager watchermanager.WatcherManager,
	optsGetter generic.RESTOptionsGetter,
	opts *options.Options,
) (*registry.Store, error) {
	gr := obj.GetGroupVersionResource().GroupResource()

	scheme.AddFieldLabelConversionFunc(
		obj.GetObjectKind().GroupVersionKind(),
		obj.FieldLabelConversion(),
	)

	var storage storebackend.Storer[runtime.Object]
	var err error
	switch opts.Type {
	case options.StorageType_File:
		storage, err = store.CreateFileStore(scheme, obj, opts.Prefix)
		if err != nil {
			return nil, err
		}
	case options.StorageType_KV:
		storage, err = store.CreateKVStore(opts.DB, scheme, obj)
		if err != nil {
			return nil, err
		}
	default:
		storage = store.CreateMemStore()
	}

	singlularResource := gr
	singlularResource.Resource = obj.GetSingularName()
	strategy := NewStrategy(obj, scheme, storage, watcherManager, opts)

	store := &registry.Store{
		Tracer:                    otel.Tracer(obj.GetSingularName()),
		NewFunc:                   obj.New,
		NewListFunc:               obj.NewList,
		PredicateFunc:             builderrest.Match,
		DefaultQualifiedResource:  gr,
		SingularQualifiedResource: singlularResource,
		GetStrategy:               strategy,
		ListStrategy:              strategy,
		CreateStrategy:            strategy,
		UpdateStrategy:            strategy,
		DeleteStrategy:            strategy,
		WatchStrategy:             strategy,
		ResetFieldsStrategy:       strategy,
		TableConvertor:            obj.TableConvertor()(gr),
		CategoryList:              obj.GetCategories(),
		ShortNameList:             obj.GetShortNames(),
		Storage:                   storage,
	}
	options := &generic.StoreOptions{
		RESTOptions: optsGetter,
		AttrFunc:    builderrest.GetAttrs,
	}
	if err := store.CompleteWithOptions(options); err != nil {
		return nil, err
	}
	return store, nil
}

func NewStatusREST(
	obj resource.Object,
	scheme *runtime.Scheme,
	watcherManager watchermanager.WatcherManager,
	opts *options.Options,
	store rest.Storage,
) (*registry.Store, error) {

	registryStore, ok := store.(*registry.Store)
	if !ok {
		return nil, fmt.Errorf("expecting registore store")
	}

	statusStore := *registryStore
	statusStore.CreateStrategy = nil
	statusStore.DeleteStrategy = nil
	statusStrategy := NewStatusStrategy(obj, scheme, registryStore.Storage, watcherManager, opts)
	statusStore.UpdateStrategy = statusStrategy
	statusStore.ResetFieldsStrategy = statusStrategy
	return &statusStore, nil
}
