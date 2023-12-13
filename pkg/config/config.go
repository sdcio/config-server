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

package config

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	api "github.com/iptecharch/config-server/apis/config/v1alpha1"
	"github.com/iptecharch/config-server/pkg/store"
	"github.com/iptecharch/config-server/pkg/target"
	"github.com/henderiw/logger/log"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/api/meta"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/watch"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/server/storage"
	"k8s.io/apiserver/pkg/storage/storagebackend"
	"sigs.k8s.io/apiserver-runtime/pkg/builder/resource"
	builderrest "sigs.k8s.io/apiserver-runtime/pkg/builder/rest"
)

var tracer = otel.Tracer("apiserver")

const (
	targetNameKey      = "targetName"
	targetNamespaceKey = "targetNamespace"
)

var _ rest.StandardStorage = &cfg{}
var _ rest.Scoper = &cfg{}
var _ rest.Storage = &cfg{}

// TODO this is to be replaced by the metadata
//var targetKey = store.GetNSNKey(types.NamespacedName{Namespace: "default", Name: "dev1"})

func NewProvider(ctx context.Context, obj resource.Object, store store.Storer[runtime.Object], targetStore store.Storer[target.Context]) builderrest.ResourceHandlerProvider {
	return func(scheme *runtime.Scheme, getter generic.RESTOptionsGetter) (rest.Storage, error) {

		fmt.Println("schema", *scheme)

		gr := obj.GetGroupVersionResource().GroupResource()
		codec, _, err := storage.NewStorageCodec(storage.StorageCodecConfig{
			StorageMediaType:  runtime.ContentTypeJSON,
			StorageSerializer: serializer.NewCodecFactory(scheme),
			StorageVersion:    scheme.PrioritizedVersionsForGroup(obj.GetGroupVersionResource().Group)[0],
			MemoryVersion:     scheme.PrioritizedVersionsForGroup(obj.GetGroupVersionResource().Group)[0],
			Config:            storagebackend.Config{}, // useless fields..
		})
		if err != nil {
			return nil, err
		}
		return NewConfigREST(
			ctx,
			store,
			targetStore,
			gr,
			codec,
			obj.NamespaceScoped(),
			obj.New,
			obj.NewList,
		), nil
	}
}

func NewConfigREST(
	ctx context.Context,
	store store.Storer[runtime.Object],
	targetStore store.Storer[target.Context],
	gr schema.GroupResource,
	//gvk schema.GroupVersionKind,
	codec runtime.Codec,
	isNamespaced bool,
	newFunc func() runtime.Object,
	newListFunc func() runtime.Object,
) rest.Storage {
	c := &cfg{
		store:          store,
		targetStore:    targetStore,
		TableConvertor: rest.NewDefaultTableConvertor(gr),
		codec:          codec,
		gr:             gr,
		//gvk:            gvk,
		isNamespaced: isNamespaced,
		newFunc:      newFunc,
		newListFunc:  newListFunc,
		watchers:     NewWatchers(32),
	}
	// start watching target changes
	targetWatcher := targetWatcher{targetStore: targetStore}
	targetWatcher.Watch(ctx)
	return c
}

type cfg struct {
	store       store.Storer[runtime.Object]
	targetStore store.Storer[target.Context]

	rest.TableConvertor
	codec runtime.Codec
	//objRootPath  string
	gr schema.GroupResource
	//gvk          schema.GroupVersionKind
	isNamespaced bool

	watchers    *watchers
	newFunc     func() runtime.Object
	newListFunc func() runtime.Object
}

func (r *cfg) Destroy() {}

func (r *cfg) New() runtime.Object {
	return r.newFunc()
}

func (r *cfg) NewList() runtime.Object {
	return r.newListFunc()
}

func (r *cfg) NamespaceScoped() bool {
	return r.isNamespaced
}

func (r *cfg) Get(
	ctx context.Context,
	name string,
	options *metav1.GetOptions,
) (runtime.Object, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configs::Get", trace.WithAttributes())
	defer span.End()

	// Get Key
	key, err := r.getKey(ctx, name)
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	log := log.FromContext(ctx).With("key", key.String())
	log.Info("get...")

	// get the data from the store
	obj, err := r.store.Get(ctx, key)
	if err != nil {
		return nil, apierrors.NewNotFound(r.gr, name)
	}
	log.Info("get succeeded", "obj", obj)
	return obj, nil
}

func getListPrt(listObj runtime.Object) (reflect.Value, error) {
	listPtr, err := meta.GetItemsPtr(listObj)
	if err != nil {
		return reflect.Value{}, err
	}
	v, err := conversion.EnforcePtr(listPtr)
	if err != nil || v.Kind() != reflect.Slice {
		return reflect.Value{}, fmt.Errorf("need ptr to slice: %v", err)
	}
	return v, nil
}

func appendItem(v reflect.Value, obj runtime.Object) {
	v.Set(reflect.Append(v, reflect.ValueOf(obj).Elem()))
}

func (r *cfg) List(
	ctx context.Context,
	options *metainternalversion.ListOptions,
) (runtime.Object, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configs::List", trace.WithAttributes())
	defer span.End()

	// logger
	log := log.FromContext(ctx)

	// Get Key
	ns, namespaced := genericapirequest.NamespaceFrom(ctx)
	if namespaced != r.isNamespaced {
		return nil, fmt.Errorf("namespace mismatch got %t, want %t", namespaced, r.isNamespaced)
	}

	newListObj := r.NewList()
	v, err := getListPrt(newListObj)
	if err != nil {
		return nil, err
	}

	r.store.List(ctx, func(ctx context.Context, key store.Key, obj runtime.Object) {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			log.Error("cannot get meta from object", "error", err.Error())
			return
		}

		if namespaced && accessor.GetNamespace() == ns {
			appendItem(v, obj)
		} else {
			appendItem(v, obj)
		}
	})

	return newListObj, nil
}

func (r *cfg) Create(
	ctx context.Context,
	runtimeObject runtime.Object,
	createValidation rest.ValidateObjectFunc,
	options *metav1.CreateOptions,
) (runtime.Object, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configs::Create", trace.WithAttributes())
	defer span.End()

	// logger
	log := log.FromContext(ctx)
	//log.Info("get", "ctx", ctx, "typeMeta", options.TypeMeta, "obj", runtimeObject)
	// setting a uid for the element
	accessor, err := meta.Accessor(runtimeObject)
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	accessor.SetUID(uuid.NewUUID())

	key, targetKey, err := r.getKeys(ctx, runtimeObject)
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	log.Info("create", "key", key.String(), "targetKey", targetKey)

	// get the data of the runtime object
	newObj, ok := runtimeObject.(*api.Config)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected Config object, got %T", runtimeObject))
	}
	log.Info("create", "obj", string(newObj.Spec.Config[0].Value.Raw))

	// interact with the data server
	tctx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil {
		return nil, apierrors.NewInternalError(errors.Wrap(err, "target not found"))
	}
	if err := tctx.SetIntent(ctx, targetKey, newObj); err != nil {
		return nil, apierrors.NewInternalError(err)
	}
	log.Info("create intent succeeded")

	if err := r.store.Create(ctx, key, runtimeObject); err != nil {
		return nil, apierrors.NewInternalError(err)
	}

	r.watchers.NotifyWatchers(watch.Event{
		Type:   watch.Added,
		Object: newObj,
	})
	return runtimeObject, nil
}

func (r *cfg) Update(
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

	// logger
	log := log.FromContext(ctx)

	// Get Key
	key, err := r.getKey(ctx, name)
	if err != nil {
		return nil, false, apierrors.NewBadRequest(err.Error())
	}
	log.Info("update", "key", key.String())

	// isCreate tracks whether this is an update that creates an object (this happens in server-side apply)
	isCreate := false

	oldObj, err := r.store.Get(ctx, key)
	if err != nil {
		log.Info("update", "err", err.Error())
		if forceAllowCreate && strings.Contains(err.Error(), "not found") {
			// For server-side apply, we can create the object here
			isCreate = true
		} else {
			return nil, false, err
		}
	}
	// get the data of the runtime object
	oldConfig, ok := oldObj.(*api.Config)
	if !ok {
		return nil, false, apierrors.NewBadRequest(fmt.Sprintf("expected old Config object, got %T", oldConfig))
	}

	fmt.Printf("ctx: %#v\n", ctx)
	fmt.Printf("objInfo: %#v\n", objInfo)
	log.Info("update", "objInfo", reflect.TypeOf(objInfo))
	log.Info("update", "oldObject", oldObj)
	newObj, err := objInfo.UpdatedObject(ctx, oldObj)
	if err != nil {
		log.Info("update failed to construct UpdatedObject", "error", err.Error())
		return nil, false, err
	}

	// get the data of the runtime object
	newConfig, ok := newObj.(*api.Config)
	if !ok {
		return nil, false, apierrors.NewBadRequest(fmt.Sprintf("expected Config object, got %T", newObj))
	}
	targetKey, err := getTargetKey(newConfig.GetLabels())
	if err != nil {
		return nil, false, apierrors.NewBadRequest(err.Error())
	}
	// interact with the data server
	tctx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}
	log.Info("delete sdc succeeded")
	log.Info("update", "key", key.String(), "targetKey", targetKey.String())
	log.Info("update", "obj", string(newConfig.Spec.Config[0].Value.Raw))

	if !isCreate {
		if err := tctx.SetIntent(ctx, targetKey, newConfig); err != nil {
			return nil, false, apierrors.NewInternalError(err)
		}
		if err := r.store.Update(ctx, key, newObj); err != nil {
			return nil, false, apierrors.NewInternalError(err)
		}
		r.watchers.NotifyWatchers(watch.Event{
			Type:   watch.Added,
			Object: newObj,
		})
		return newObj, false, nil
	}
	if err := tctx.SetIntent(ctx, targetKey, newConfig); err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}
	if err := r.store.Create(ctx, key, newObj); err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}
	r.watchers.NotifyWatchers(watch.Event{
		Type:   watch.Modified,
		Object: newObj,
	})
	return newObj, true, nil

}

func (r *cfg) Delete(
	ctx context.Context,
	name string,
	deleteValidation rest.ValidateObjectFunc,
	options *metav1.DeleteOptions,
) (runtime.Object, bool, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configs::Delete", trace.WithAttributes())
	defer span.End()

	// logger
	log := log.FromContext(ctx)

	// Get Key
	key, err := r.getKey(ctx, name)
	if err != nil {
		return nil, false, apierrors.NewBadRequest(err.Error())
	}
	log.Info("delete", "key", key.String())

	obj, err := r.store.Get(ctx, key)
	if err != nil {
		return nil, false, apierrors.NewNotFound(r.gr, name)
	}

	// get the data of the runtime object
	newConfig, ok := obj.(*api.Config)
	if !ok {
		return nil, false, apierrors.NewBadRequest(fmt.Sprintf("expected Config object, got %T", obj))
	}
	targetKey, err := getTargetKey(newConfig.GetLabels())
	if err != nil {
		return nil, false, apierrors.NewBadRequest(err.Error())
	}

	// interact with the data server
	tctx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}
	if err := tctx.DeleteIntent(ctx, targetKey, newConfig); err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}
	log.Info("delete intent succeeded")

	if err := r.store.Delete(ctx, key); err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}
	r.watchers.NotifyWatchers(watch.Event{
		Type:   watch.Modified,
		Object: obj,
	})

	return obj, true, nil
}

func (r *cfg) DeleteCollection(
	ctx context.Context,
	deleteValidation rest.ValidateObjectFunc,
	options *metav1.DeleteOptions,
	listOptions *metainternalversion.ListOptions,
) (runtime.Object, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configs::DeleteCollection", trace.WithAttributes())
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

	r.store.List(ctx, func(ctx context.Context, key store.Key, obj runtime.Object) {
		// TODO delete
		appendItem(v, obj)
	})

	return newListObj, nil
}

func (r *cfg) Watch(
	ctx context.Context,
	options *metainternalversion.ListOptions,
) (watch.Interface, error) {
	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configs::Watch", trace.WithAttributes())
	defer span.End()

	// logger
	log := log.FromContext(ctx)
	log.Info("watch", "options", *options)

	if r.watchers.IsExhausted() {
		return nil, fmt.Errorf("cannot allocate watcher, out of resources")
	}
	w := &mWatch{
		watchers: r.watchers,
		resultCh: make(chan watch.Event, 10),
	}
	// On initial watch, send all the existing objects
	list, err := r.List(ctx, options)
	if err != nil {
		return nil, err
	}

	items := reflect.ValueOf(list).Elem().FieldByName("Items")
	for i := 0; i < items.Len(); i++ {
		obj := items.Index(i).Addr().Interface().(runtime.Object)
		w.resultCh <- watch.Event{
			Type:   watch.Added,
			Object: obj,
		}
	}
	// this ensures the initial events from the list
	// get processed first
	if err := r.watchers.Add(w); err != nil {
		return nil, err
	}

	return w, nil
}
