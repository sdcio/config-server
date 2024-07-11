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

import (
	"context"

	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	builderrest "github.com/henderiw/apiserver-builder/pkg/builder/rest"
	"github.com/henderiw/apiserver-builder/pkg/builder/utils"
	"github.com/henderiw/apiserver-store/pkg/generic/registry"
	watchermanager "github.com/henderiw/apiserver-store/pkg/watcher-manager"
	"github.com/sdcio/config-server/pkg/registry/options"
	"go.opentelemetry.io/otel"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/rest"
)

func NewStorageProvider(ctx context.Context, obj resource.InternalObject, opts *options.Options) *builderrest.StorageProvider {
	watcherManager := watchermanager.New(64)

	go watcherManager.Start(ctx)

	sp := &builderrest.StorageProvider{
		ResourceStorageProviderFn: func(scheme *runtime.Scheme, optsGetter generic.RESTOptionsGetter) (rest.Storage, error) {
			return NewREST(obj, scheme, watcherManager, optsGetter, opts)
		},
	}
	return sp
}

func NewREST(
	obj resource.InternalObject,
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

	singlularResource := gr
	singlularResource.Resource = obj.GetSingularName()
	strategy := NewStrategy(obj, scheme, nil, watcherManager, opts)

	store := &registry.Store{
		Tracer:                    otel.Tracer(obj.GetSingularName()),
		NewFunc:                   obj.New,
		NewListFunc:               obj.NewList,
		PredicateFunc:             utils.Match,
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
		Storage:                   nil,
	}
	options := &generic.StoreOptions{
		RESTOptions: optsGetter,
		AttrFunc:    utils.GetAttrs,
	}
	if err := store.CompleteWithOptions(options); err != nil {
		return nil, err
	}
	return store, nil
}
