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
	"errors"

	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	"github.com/henderiw/apiserver-builder/pkg/builder/utils"
	"github.com/henderiw/apiserver-store/pkg/rest"
	"github.com/henderiw/apiserver-store/pkg/storebackend"
	watchermanager "github.com/henderiw/apiserver-store/pkg/watcher-manager"
	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/apis/config"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/registry/options"
	"github.com/sdcio/config-server/pkg/target"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/watch"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/storage/names"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/structured-merge-diff/v4/fieldpath"
)

// NewStrategy creates and returns a strategy instance
func NewStrategy(
	obj resource.InternalObject,
	typer runtime.ObjectTyper,
	storage storebackend.Storer[runtime.Object],
	watcherManager watchermanager.WatcherManager,
	opts *options.Options,
) *strategy {

	return &strategy{
		ObjectTyper:    typer,
		NameGenerator:  names.SimpleNameGenerator,
		gr:             obj.GetGroupVersionResource().GroupResource(),
		obj:            obj,
		storage:        storage,
		watcherManager: watcherManager,
		client:         opts.Client,
		targetStore:    opts.TargetStore,
	}
}

var _ rest.RESTGetStrategy = &strategy{}
var _ rest.RESTListStrategy = &strategy{}
var _ rest.RESTCreateStrategy = &strategy{}
var _ rest.RESTUpdateStrategy = &strategy{}
var _ rest.RESTDeleteStrategy = &strategy{}
var _ rest.RESTWatchStrategy = &strategy{}
var _ rest.ResetFieldsStrategy = &strategy{}

type strategy struct {
	runtime.ObjectTyper
	names.NameGenerator
	gr             schema.GroupResource
	obj            resource.InternalObject
	storage        storebackend.Storer[runtime.Object]
	watcherManager watchermanager.WatcherManager
	client         client.Client
	targetStore    storebackend.Storer[*target.Context]
}

func (r *strategy) NamespaceScoped() bool { return r.obj.NamespaceScoped() }

func (r *strategy) Canonicalize(obj runtime.Object) {}

func (r *strategy) BeginCreate(ctx context.Context) error {
	return apierrors.NewMethodNotSupported(r.obj.GetGroupVersionResource().GroupResource(), "create")
}

func (r *strategy) PrepareForCreate(ctx context.Context, obj runtime.Object) {
	r.obj.PrepareForCreate(ctx, obj)
}

func (r *strategy) Validate(ctx context.Context, obj runtime.Object) field.ErrorList {
	return r.obj.ValidateCreate(ctx, obj)
}

func (r *strategy) Create(ctx context.Context, key types.NamespacedName, obj runtime.Object, dryrun bool) (runtime.Object, error) {
	return obj, apierrors.NewMethodNotSupported(r.obj.GetGroupVersionResource().GroupResource(), "create")
}

func (r *strategy) WarningsOnCreate(ctx context.Context, obj runtime.Object) []string {
	return nil
}

func (r *strategy) BeginUpdate(ctx context.Context) error {
	return apierrors.NewMethodNotSupported(r.obj.GetGroupVersionResource().GroupResource(), "update")
}

func (r *strategy) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
	r.obj.PrepareForUpdate(ctx, obj, old)
}

func (r *strategy) AllowCreateOnUpdate() bool { return false }

func (r *strategy) AllowUnconditionalUpdate() bool { return false }

func (r *strategy) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	return r.obj.ValidateUpdate(ctx, obj, old)
}

func (r *strategy) Update(ctx context.Context, key types.NamespacedName, obj, old runtime.Object, dryrun bool) (runtime.Object, error) {
	return obj, apierrors.NewMethodNotSupported(r.obj.GetGroupVersionResource().GroupResource(), "update")
}

func (r *strategy) WarningsOnUpdate(ctx context.Context, obj, old runtime.Object) []string {
	return nil
}

func (r *strategy) BeginDelete(ctx context.Context) error { return nil }

func (r *strategy) Delete(ctx context.Context, key types.NamespacedName, obj runtime.Object, dryrun bool) (runtime.Object, error) {
	return obj, nil
}

func (r *strategy) Get(ctx context.Context, key types.NamespacedName) (runtime.Object, error) {
	target, tctx, err := r.getTargetContext(ctx, key)
	if err != nil {
		return nil, apierrors.NewNotFound(r.gr, key.Name)
	}

	rc, err := tctx.GetData(ctx, storebackend.KeyFromNSN(key))
	if err != nil {
		return nil, apierrors.NewInternalError(err)
	}
	rc.SetCreationTimestamp(target.CreationTimestamp)
	rc.SetResourceVersion(target.ResourceVersion)
	rc.SetAnnotations(target.Annotations)
	rc.SetLabels(target.Labels)
	obj := rc

	return obj, nil
}

func (r *strategy) List(ctx context.Context, options *metainternalversion.ListOptions) (runtime.Object, error) {
	log := log.FromContext(ctx)

	filter, err := parseFieldSelector(ctx, options.FieldSelector)
	if err != nil {
		return nil, err
	}

	namespace, ok := genericapirequest.NamespaceFrom(ctx)
	if ok {
		if filter != nil {
			filter.Namespace = namespace
		} else {
			filter = &Filter{
				Namespace: namespace,
			}
		}
	}

	newListObj := r.obj.NewList()
	v, err := utils.GetListPrt(newListObj)
	if err != nil {
		return nil, err
	}

	runningConfigListFunc := func(ctx context.Context, key storebackend.Key, tctx *target.Context) {
		target := &invv1alpha1.Target{}
		if err := r.client.Get(ctx, key.NamespacedName, target); err != nil {
			log.Error("cannot get target", "key", key.String(), "error", err.Error())
			return
		}

		if options.LabelSelector != nil || filter != nil {
			f := true
			if options.LabelSelector != nil {
				if options.LabelSelector.Matches(labels.Set(target.GetLabels())) {
					f = false
				}
			} else {
				// if not labels selector is present don't filter
				f = false
			}
			// if filtered we dont have to run this section since the label requirement was not met
			if filter != nil && !f {
				if filter.Name != "" {
					if target.GetName() == filter.Name {
						f = false
					} else {
						f = true
					}
				}
				if filter.Namespace != "" {
					if target.GetNamespace() == filter.Namespace {
						f = false
					} else {
						f = true
					}
				}
			}
			if !f {
				obj, err := tctx.GetData(ctx, key)
				if err != nil {
					log.Error("cannot get running config", "key", key.String(), "error", err.Error())
					return
				}
				obj.SetCreationTimestamp(target.CreationTimestamp)
				obj.SetResourceVersion(target.ResourceVersion)
				obj.SetAnnotations(target.Annotations)
				obj.SetLabels(target.Labels)
				utils.AppendItem(v, obj)
			}
		} else {
			obj, err := tctx.GetData(ctx, key)
			if err != nil {
				log.Error("cannot get running config", "key", key.String(), "error", err.Error())
				return
			}
			obj.SetCreationTimestamp(target.CreationTimestamp)
			obj.SetResourceVersion(target.ResourceVersion)
			obj.SetAnnotations(target.Annotations)
			obj.SetLabels(target.Labels)
			utils.AppendItem(v, obj)
		}
	}

	r.targetStore.List(ctx, runningConfigListFunc)
	return newListObj, nil
}

func (r *strategy) BeginWatch(ctx context.Context) error { return nil }

func (r *strategy) Watch(ctx context.Context, options *metainternalversion.ListOptions) (watch.Interface, error) {
	ctx, cancel := context.WithCancel(ctx)

	w := &watcher{
		cancel:         cancel,
		resultChan:     make(chan watch.Event),
		watcherManager: r.watcherManager,
	}

	go w.listAndWatch(ctx, r, options)

	return w, nil
}

// GetResetFields returns the set of fields that get reset by the strategy
// and should not be modified by the user.
func (r *strategy) GetResetFields() map[fieldpath.APIVersion]*fieldpath.Set {
	fields := map[fieldpath.APIVersion]*fieldpath.Set{
		fieldpath.APIVersion(r.obj.GetGroupVersionResource().GroupVersion().String()): fieldpath.NewSet(
			fieldpath.MakePathOrDie("status"),
		),
	}

	return fields
}

func (r *strategy) getTargetContext(ctx context.Context, targetKey types.NamespacedName) (*invv1alpha1.Target, *target.Context, error) {
	target := &invv1alpha1.Target{}
	if err := r.client.Get(ctx, targetKey, target); err != nil {
		return nil, nil, err
	}
	if !target.IsReady() {
		return nil, nil, errors.New(string(config.ConditionReasonTargetNotReady))
	}
	tctx, err := r.targetStore.Get(ctx, storebackend.Key{NamespacedName: targetKey})
	if err != nil {
		return nil, nil, errors.New(string(config.ConditionReasonTargetNotFound))
	}
	return target, tctx, nil
}
