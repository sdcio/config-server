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

	"github.com/henderiw/logger/log"
	configv1alpha1 "github.com/iptecharch/config-server/apis/config/v1alpha1"
	"github.com/iptecharch/config-server/pkg/store"
	watchermanager "github.com/iptecharch/config-server/pkg/watcher-manager"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/rest"
	"github.com/henderiw/apiserver-builder/pkg/builder/resource"

	builderrest "github.com/henderiw/apiserver-builder/pkg/builder/rest"
)

var tracer = otel.Tracer("config-server")

const (
	targetNameKey      = "targetName"
	targetNamespaceKey = "targetNamespace"
)

var _ rest.StandardStorage = &config{}
var _ rest.Scoper = &config{}
var _ rest.Storage = &config{}
var _ rest.TableConvertor = &config{}
var _ rest.SingularNameProvider = &config{}

func NewConfigProvider(ctx context.Context, obj resource.Object, cfg *Cfg) builderrest.ResourceHandlerProvider {
	return func(ctx context.Context, scheme *runtime.Scheme, getter generic.RESTOptionsGetter) (rest.Storage, error) {
		gr := obj.GetGroupVersionResource().GroupResource()
		return NewConfigREST(
			ctx,
			cfg,
			gr,
			obj.NamespaceScoped(),
			obj.New,
			obj.NewList,
		), nil
	}
}

func NewConfigREST(
	ctx context.Context,
	cfg *Cfg,
	gr schema.GroupResource,
	isNamespaced bool,
	newFunc func() runtime.Object,
	newListFunc func() runtime.Object,
) rest.Storage {
	c := &config{
		configCommon: configCommon{
			client:         cfg.client,
			configStore:    cfg.configStore,
			configSetStore: cfg.configSetStore,
			targetStore:    cfg.targetStore,
			gr:             gr,
			isNamespaced:   isNamespaced,
			newFunc:        newFunc,
			newListFunc:    newListFunc,
		},
		TableConvertor: NewConfigTableConvertor(gr),
		watcherManager: watchermanager.New(32),
	}
	go c.watcherManager.Start(ctx)
	// start watching target changes
	targetWatcher := targetWatcher{targetStore: cfg.targetStore}
	targetWatcher.Watch(ctx)
	return c
}

type config struct {
	configCommon
	rest.TableConvertor
	watcherManager watchermanager.WatcherManager
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
	r.notifyWatcher(ctx, watch.Event{
		Type:   watch.Added,
		Object: obj,
	})
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
	r.notifyWatcher(ctx, watch.Event{
		Type:   watch.Deleted,
		Object: obj,
	})
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

	// logger
	log := log.FromContext(ctx)

	if options.FieldSelector == nil {
		log.Info("watch", "options", *options, "fieldselector", "nil")
	} else {
		requirements := options.FieldSelector.Requirements()
		log.Info("watch", "options", *options, "fieldselector", options.FieldSelector.Requirements())
		for _, requirement := range requirements {
			log.Info("watch requirement",
				"Operator", requirement.Operator,
				"Value", requirement.Value,
				"Field", requirement.Field,
			)
		}
	}

	ctx, cancel := context.WithCancel(ctx)

	w := &watcher{
		cancel:         cancel,
		resultChan:     make(chan watch.Event),
		watcherManager: r.watcherManager,
	}

	go w.listAndWatch(ctx, r, options)

	return w, nil
}

func (r *config) notifyWatcher(ctx context.Context, event watch.Event) {
	log := log.FromContext(ctx).With("eventType", event.Type)
	log.Info("notify watcherManager")

	r.watcherManager.WatchChan() <- event
}
