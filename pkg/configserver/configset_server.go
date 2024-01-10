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
	"github.com/iptecharch/config-server/pkg/store"
	watchermanager "github.com/iptecharch/config-server/pkg/watcher-manager"
	"go.opentelemetry.io/otel/trace"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/rest"
	"sigs.k8s.io/apiserver-runtime/pkg/builder/resource"
	builderrest "sigs.k8s.io/apiserver-runtime/pkg/builder/rest"
)

var _ rest.StandardStorage = &configset{}
var _ rest.Scoper = &configset{}
var _ rest.Storage = &configset{}
var _ rest.TableConvertor = &configset{}

func NewConfigSetProvider(ctx context.Context, obj resource.Object, cfg *Config) builderrest.ResourceHandlerProvider {
	return func(scheme *runtime.Scheme, getter generic.RESTOptionsGetter) (rest.Storage, error) {
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

func NewConfigSetREST(
	ctx context.Context,
	cfg *Config,
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
		TableConvertor: NewConfigSetTableConvertor(gr),
		watcherManager: watchermanager.New(32),
	}
	go c.watcherManager.Start(ctx)
	// start watching target changes
	targetWatcher := targetWatcher{targetStore: cfg.targetStore}
	targetWatcher.Watch(ctx)
	return c
}

type configset struct {
	configCommon
	rest.TableConvertor
	watcherManager watchermanager.WatcherManager
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

func (r *configset) Get(
	ctx context.Context,
	name string,
	options *metav1.GetOptions,
) (runtime.Object, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configsets::Get", trace.WithAttributes())
	defer span.End()

	return r.get(ctx, name, options)
}

func (r *configset) List(
	ctx context.Context,
	options *metainternalversion.ListOptions,
) (runtime.Object, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configsets::List", trace.WithAttributes())
	defer span.End()

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

	// logger
	obj, err := r.createConfigSet(ctx, runtimeObject, createValidation, options)
	if err != nil {
		return obj, err
	}
	r.notifyWatcher(ctx, watch.Event{
		Type:   watch.Added,
		Object: obj,
	})
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

	obj, create, err := r.updateConfigSet(ctx, name, objInfo, createValidation, updateValidation, forceAllowCreate, options)
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

func (r *configset) Delete(
	ctx context.Context,
	name string,
	deleteValidation rest.ValidateObjectFunc,
	options *metav1.DeleteOptions,
) (runtime.Object, bool, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configsets::Delete", trace.WithAttributes())
	defer span.End()

	obj, asyncDelete, err := r.deleteConfigSet(ctx, name, deleteValidation, options)
	if err != nil {
		return obj, asyncDelete, err
	}
	r.notifyWatcher(ctx, watch.Event{
		Type:   watch.Deleted,
		Object: obj,
	})
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

func (r *configset) notifyWatcher(ctx context.Context, event watch.Event) {
	log := log.FromContext(ctx).With("eventType", event.Type)
	log.Info("notify watcherManager")

	r.watcherManager.WatchChan() <- event
}
