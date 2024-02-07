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

package configserver

import (
	"context"
	"fmt"
	"reflect"

	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	"github.com/henderiw/logger/log"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/sdcio/config-server/pkg/store"
	"github.com/sdcio/config-server/pkg/target"
	watchermanager "github.com/sdcio/config-server/pkg/watcher-manager"
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

func NewConfigProviderHandler(ctx context.Context, p ResourceProvider) builderrest.ResourceHandlerProvider {
	return func(ctx context.Context, scheme *runtime.Scheme, getter generic.RESTOptionsGetter) (rest.Storage, error) {
		return p, nil
	}
}

func NewConfigFileProvider(
	ctx context.Context,
	obj resource.Object,
	scheme *runtime.Scheme,
	client client.Client,
	targetStore store.Storer[target.Context]) (ResourceProvider, error) {
	configStore, err := createFileStore(ctx, obj, rootConfigFilePath)
	if err != nil {
		return nil, err
	}
	return newConfigProvider(ctx, obj, configStore, client, targetStore)
}

func NewConfigMemProvider(
	ctx context.Context,
	obj resource.Object,
	scheme *runtime.Scheme,
	client client.Client,
	targetStore store.Storer[target.Context]) (ResourceProvider, error) {

	return newConfigProvider(ctx, obj, createMemStore(ctx), client, targetStore)
}

func newConfigProvider(
	ctx context.Context,
	obj resource.Object,
	configStore store.Storer[runtime.Object],
	client client.Client,
	targetStore store.Storer[target.Context]) (ResourceProvider, error) {
	// initialie the rest storage object
	gr := obj.GetGroupVersionResource().GroupResource()
	c := &config{
		configCommon: configCommon{
			//configSetStore -> no needed for config, only used by configset
			client:         client,
			configStore:    configStore,
			targetStore:    targetStore,
			gr:             gr,
			isNamespaced:   obj.NamespaceScoped(),
			newFunc:        obj.New,
			newListFunc:    obj.NewList,
			watcherManager: watchermanager.New(32),
		},
		TableConvertor: NewConfigTableConvertor(gr),
	}
	go c.configCommon.watcherManager.Start(ctx)
	return c, nil
}

var _ rest.StandardStorage = &config{}
var _ rest.Scoper = &config{}
var _ rest.Storage = &config{}
var _ rest.TableConvertor = &config{}
var _ rest.SingularNameProvider = &config{}

type config struct {
	configCommon
	rest.TableConvertor
}

func (r *config) GetStore() store.Storer[runtime.Object] { return r.configStore }

func (r *config) UpdateStore(ctx context.Context, key store.Key, obj runtime.Object) error {
	config, ok := obj.(*configv1alpha1.Config)
	if !ok {
		return fmt.Errorf("expected Config object, got %T", obj)
	}
	return r.storeUpdateConfig(ctx, key, config)
}

func (r *config) Apply(ctx context.Context, key store.Key, targetKey store.Key, oldObj, newObj runtime.Object) error {
	oldConfig, ok := oldObj.(*configv1alpha1.Config)
	if !ok {
		return fmt.Errorf("apply unexpected old object, want: %s, got: %s", configv1alpha1.ConfigKind, reflect.TypeOf(oldObj).Name())
	}
	newConfig, ok := newObj.(*configv1alpha1.Config)
	if !ok {
		return fmt.Errorf("apply unexpected new object, want: %s, got: %s", configv1alpha1.ConfigKind, reflect.TypeOf(newObj).Name())
	}
	_, _, err := r.upsertTargetConfig(ctx, key, targetKey, oldConfig, newConfig, true)
	return err
}

func (r *config) SetIntent(ctx context.Context, key store.Key, targetKey store.Key, tctx *target.Context, newObj runtime.Object) error {
	newConfig, ok := newObj.(*configv1alpha1.Config)
	if !ok {
		return fmt.Errorf("setIntent unexpected new object, want: %s, got: %s", configv1alpha1.ConfigKind, reflect.TypeOf(newObj).Name())
	}
	return r.setIntent(ctx, key, targetKey, tctx, newConfig, false)
}

func (r *config) Destroy() {}

func (r *config) New() runtime.Object {
	return r.newFunc()
}

func (r *config) NewList() runtime.Object {
	return r.newListFunc()
}

func (r *config) NamespaceScoped() bool {
	return true
}

func (r *config) GetSingularName() string {
	return "config"
}

func (r *config) Get(
	ctx context.Context,
	name string,
	options *metav1.GetOptions,
) (runtime.Object, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configs::Get", trace.WithAttributes())
	defer span.End()

	options.TypeMeta = metav1.TypeMeta{APIVersion: configv1alpha1.SchemeBuilder.GroupVersion.Identifier(), Kind: configv1alpha1.ConfigKind}

	return r.get(ctx, name, options)
}

func (r *config) List(
	ctx context.Context,
	options *metainternalversion.ListOptions,
) (runtime.Object, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configs::List", trace.WithAttributes())
	defer span.End()

	options.TypeMeta = metav1.TypeMeta{APIVersion: configv1alpha1.SchemeBuilder.GroupVersion.Identifier(), Kind: configv1alpha1.ConfigKind}

	return r.list(ctx, options)
}

func (r *config) Create(
	ctx context.Context,
	runtimeObject runtime.Object,
	createValidation rest.ValidateObjectFunc,
	options *metav1.CreateOptions,
) (runtime.Object, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configs::Create", trace.WithAttributes())
	defer span.End()

	options.TypeMeta = metav1.TypeMeta{APIVersion: configv1alpha1.SchemeBuilder.GroupVersion.Identifier(), Kind: configv1alpha1.ConfigKind}

	// logger
	obj, err := r.createConfig(ctx, runtimeObject, createValidation, options)
	if err != nil {
		return obj, err
	}
	/*
		r.notifyWatcher(ctx, watch.Event{
			Type:   watch.Added,
			Object: obj,
		})
	*/
	return obj, nil
}

func (r *config) Update(
	ctx context.Context,
	name string,
	objInfo rest.UpdatedObjectInfo,
	createValidation rest.ValidateObjectFunc,
	updateValidation rest.ValidateObjectUpdateFunc,
	forceAllowCreate bool,
	options *metav1.UpdateOptions,
) (runtime.Object, bool, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configs::Update", trace.WithAttributes())
	defer span.End()

	options.TypeMeta = metav1.TypeMeta{APIVersion: configv1alpha1.SchemeBuilder.GroupVersion.Identifier(), Kind: configv1alpha1.ConfigKind}

	obj, create, err := r.updateConfig(ctx, name, objInfo, createValidation, updateValidation, forceAllowCreate, options)
	if err != nil {
		return obj, create, err
	}
	/*
		if create {
			r.notifyWatcher(ctx, watch.Event{
				Type:   watch.Added,
				Object: obj,
			})
		} else {
			r.notifyWatcher(ctx, watch.Event{
				Type:   watch.Modified,
				Object: obj,
			})
		}
	*/
	return obj, create, nil
}

func (r *config) Delete(
	ctx context.Context,
	name string,
	deleteValidation rest.ValidateObjectFunc,
	options *metav1.DeleteOptions,
) (runtime.Object, bool, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configs::Delete", trace.WithAttributes())
	defer span.End()

	options.TypeMeta = metav1.TypeMeta{APIVersion: configv1alpha1.SchemeBuilder.GroupVersion.Identifier(), Kind: configv1alpha1.ConfigKind}

	obj, asyncDelete, err := r.deleteConfig(ctx, name, deleteValidation, options)
	if err != nil {
		return obj, asyncDelete, err
	}
	/*
		r.notifyWatcher(ctx, watch.Event{
			Type:   watch.Deleted,
			Object: obj,
		})
	*/
	return obj, asyncDelete, nil
}

func (r *config) DeleteCollection(
	ctx context.Context,
	deleteValidation rest.ValidateObjectFunc,
	options *metav1.DeleteOptions,
	listOptions *metainternalversion.ListOptions,
) (runtime.Object, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configs::DeleteCollection", trace.WithAttributes())
	defer span.End()

	options.TypeMeta = metav1.TypeMeta{APIVersion: configv1alpha1.SchemeBuilder.GroupVersion.Identifier(), Kind: configv1alpha1.ConfigKind}

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

func (r *config) Watch(
	ctx context.Context,
	options *metainternalversion.ListOptions,
) (watch.Interface, error) {
	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configs::Watch", trace.WithAttributes())
	defer span.End()

	options.TypeMeta = metav1.TypeMeta{APIVersion: configv1alpha1.SchemeBuilder.GroupVersion.Identifier(), Kind: configv1alpha1.ConfigKind}

	w := r.watch(ctx, options)

	return w, nil
}
