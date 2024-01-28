// Copyright 2023 The xxx Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package configserver

import (
	"context"
	"fmt"
	"reflect"

	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	"github.com/henderiw/logger/log"
	configv1alpha1 "github.com/iptecharch/config-server/apis/config/v1alpha1"
	"github.com/iptecharch/config-server/pkg/store"
	"github.com/iptecharch/config-server/pkg/target"
	watchermanager "github.com/iptecharch/config-server/pkg/watcher-manager"
	"go.opentelemetry.io/otel/trace"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	builderrest "github.com/henderiw/apiserver-builder/pkg/builder/rest"
)

func NewConfigSetProviderHandler(ctx context.Context, s ResourceProvider) builderrest.ResourceHandlerProvider {
	return func(ctx context.Context, scheme *runtime.Scheme, getter generic.RESTOptionsGetter) (rest.Storage, error) {
		return s, nil
	}
}

func NewConfigSetFileProvider(
	ctx context.Context,
	obj resource.Object,
	scheme *runtime.Scheme,
	client client.Client,
	configStore store.Storer[runtime.Object],
	targetStore store.Storer[target.Context]) (ResourceProvider, error) {

	configSetStore, err := createFileStore(ctx, obj, rootConfigFilePath)
	if err != nil {
		return nil, err
	}
	return newConfigSetProvider(ctx, obj, configSetStore, client, configStore, targetStore)
}

func NewConfigSetMemProvider(
	ctx context.Context,
	obj resource.Object,
	scheme *runtime.Scheme,
	client client.Client,
	configStore store.Storer[runtime.Object],
	targetStore store.Storer[target.Context]) (ResourceProvider, error) {

	return newConfigSetProvider(ctx, obj, createMemStore(ctx), client, configStore, targetStore)
}

func newConfigSetProvider(
	ctx context.Context,
	obj resource.Object,
	configSetStore store.Storer[runtime.Object],
	client client.Client,
	configStore store.Storer[runtime.Object],
	targetStore store.Storer[target.Context]) (ResourceProvider, error) {
	// create the backend store

	// initialie the rest storage object
	gr := obj.GetGroupVersionResource().GroupResource()
	c := &configset{
		configCommon: configCommon{
			client:         client,
			configStore:    configStore,
			configSetStore: configSetStore,
			targetStore:    targetStore, // needed as we handle configs from configsets
			gr:             gr,
			isNamespaced:   obj.NamespaceScoped(),
			newFunc:        obj.New,
			newListFunc:    obj.NewList,
			watcherManager: watchermanager.New(32),
		},
		TableConvertor: NewConfigSetTableConvertor(gr),
		//watcherManager: watcherManager,
	}
	go c.configCommon.watcherManager.Start(ctx)
	return c, nil
}

var _ rest.StandardStorage = &configset{}
var _ rest.Scoper = &configset{}
var _ rest.Storage = &configset{}
var _ rest.TableConvertor = &configset{}
var _ rest.SingularNameProvider = &configset{}

type configset struct {
	configCommon
	rest.TableConvertor
	//watcherManager watchermanager.WatcherManager
}

func (r *configset) GetStore() store.Storer[runtime.Object] { return r.configSetStore }

func (r *configset) UpdateStore(ctx context.Context, key store.Key, obj runtime.Object) error {
	return r.configSetStore.Update(ctx, key, obj)
}

func (r *configset) Apply(ctx context.Context, key store.Key, targetKey store.Key, oldObj, newObj runtime.Object) error {
	newConfigSet, ok := newObj.(*configv1alpha1.ConfigSet)
	if !ok {
		return fmt.Errorf("apply unexpected new object, want: %s, got: %s", configv1alpha1.ConfigSetKind, reflect.TypeOf(newObj).Name())
	}
	_, err := r.upsertConfigSet(ctx, newConfigSet)
	return err
}

func (r *configset) SetIntent(ctx context.Context, key store.Key, targetKey store.Key, tctx *target.Context, newObj runtime.Object) error {
	return fmt.Errorf("setIntent not supported for confgisets")
}

func (r *configset) Destroy() {}

func (r *configset) New() runtime.Object {
	return r.newFunc()
}

func (r *configset) NewList() runtime.Object {
	return r.newListFunc()
}

func (r *configset) NamespaceScoped() bool {
	return r.isNamespaced
}

func (r *configset) GetSingularName() string {
	return "configset"
}

func (r *configset) Get(
	ctx context.Context,
	name string,
	options *metav1.GetOptions,
) (runtime.Object, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configsets::Get", trace.WithAttributes())
	defer span.End()

	options.TypeMeta = metav1.TypeMeta{APIVersion: configv1alpha1.SchemeBuilder.GroupVersion.Identifier(), Kind: configv1alpha1.ConfigSetKind}

	return r.get(ctx, name, options)
}

func (r *configset) List(
	ctx context.Context,
	options *metainternalversion.ListOptions,
) (runtime.Object, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configsets::List", trace.WithAttributes())
	defer span.End()

	options.TypeMeta = metav1.TypeMeta{APIVersion: configv1alpha1.SchemeBuilder.GroupVersion.Identifier(), Kind: configv1alpha1.ConfigSetKind}

	return r.list(ctx, options)
}

func (r *configset) Create(
	ctx context.Context,
	runtimeObject runtime.Object,
	createValidation rest.ValidateObjectFunc,
	options *metav1.CreateOptions,
) (runtime.Object, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configsets::Create", trace.WithAttributes())
	defer span.End()

	options.TypeMeta = metav1.TypeMeta{APIVersion: configv1alpha1.SchemeBuilder.GroupVersion.Identifier(), Kind: configv1alpha1.ConfigSetKind}

	// logger
	obj, err := r.createConfigSet(ctx, runtimeObject, createValidation, options)
	if err != nil {
		return obj, err
	}
	return obj, nil
}

func (r *configset) Update(
	ctx context.Context,
	name string,
	objInfo rest.UpdatedObjectInfo,
	createValidation rest.ValidateObjectFunc,
	updateValidation rest.ValidateObjectUpdateFunc,
	forceAllowCreate bool,
	options *metav1.UpdateOptions,
) (runtime.Object, bool, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configsets::Update", trace.WithAttributes())
	defer span.End()

	options.TypeMeta = metav1.TypeMeta{APIVersion: configv1alpha1.SchemeBuilder.GroupVersion.Identifier(), Kind: configv1alpha1.ConfigSetKind}

	obj, create, err := r.updateConfigSet(ctx, name, objInfo, createValidation, updateValidation, forceAllowCreate, options)
	if err != nil {
		return obj, create, err
	}
	return obj, create, nil
}

func (r *configset) Delete(
	ctx context.Context,
	name string,
	deleteValidation rest.ValidateObjectFunc,
	options *metav1.DeleteOptions,
) (runtime.Object, bool, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configsets::Delete", trace.WithAttributes())
	defer span.End()

	options.TypeMeta = metav1.TypeMeta{APIVersion: configv1alpha1.SchemeBuilder.GroupVersion.Identifier(), Kind: configv1alpha1.ConfigSetKind}

	obj, asyncDelete, err := r.deleteConfigSet(ctx, name, deleteValidation, options)
	if err != nil {
		return obj, asyncDelete, err
	}
	return obj, asyncDelete, nil
}

func (r *configset) DeleteCollection(
	ctx context.Context,
	deleteValidation rest.ValidateObjectFunc,
	options *metav1.DeleteOptions,
	listOptions *metainternalversion.ListOptions,
) (runtime.Object, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configsets::DeleteCollection", trace.WithAttributes())
	defer span.End()

	options.TypeMeta = metav1.TypeMeta{APIVersion: configv1alpha1.SchemeBuilder.GroupVersion.Identifier(), Kind: configv1alpha1.ConfigSetKind}

	// logger
	log := log.FromContext(ctx)
	log.Info("delete collection")

	// Get Key
	key, err := r.getKey(ctx, "")
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	log.Info("delete collection", "key", key.String())

	newListObj := r.NewList()
	v, err := getListPrt(newListObj)
	if err != nil {
		return nil, err
	}

	r.configStore.List(ctx, func(ctx context.Context, key store.Key, obj runtime.Object) {
		// TODO delete
		appendItem(v, obj)
	})

	return newListObj, nil
}

func (r *configset) Watch(
	ctx context.Context,
	options *metainternalversion.ListOptions,
) (watch.Interface, error) {
	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configsets::Watch", trace.WithAttributes())
	defer span.End()

	options.TypeMeta = metav1.TypeMeta{APIVersion: configv1alpha1.SchemeBuilder.GroupVersion.Identifier(), Kind: configv1alpha1.ConfigSetKind}

	w := r.watch(ctx, options)

	return w, nil
}

/*
func (r *configset) notifyWatcher(ctx context.Context, event watch.Event) {
	log := log.FromContext(ctx).With("eventType", event.Type)
	log.Info("notify watcherManager")

	r.watcherManager.WatchChan() <- event
}
*/
