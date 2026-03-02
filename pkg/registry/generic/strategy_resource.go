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

package generic

import (
	"context"

	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	"github.com/henderiw/apiserver-builder/pkg/builder/utils"
	"github.com/henderiw/apiserver-store/pkg/rest"
	"github.com/henderiw/apiserver-store/pkg/storebackend"
	watchermanager "github.com/henderiw/apiserver-store/pkg/watcher-manager"
	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/pkg/registry/options"
	"go.yaml.in/yaml/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/watch"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/storage/names"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"
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
		opts:           opts,
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
	opts           *options.Options
}

func (r *strategy) NamespaceScoped() bool { return r.obj.NamespaceScoped() }

func (r *strategy) Canonicalize(obj runtime.Object) {}

func (r *strategy) Get(ctx context.Context, key types.NamespacedName) (runtime.Object, error) {
	obj, err := r.storage.Get(ctx, storebackend.KeyFromNSN(key))
	if err != nil {
		return nil, apierrors.NewNotFound(r.gr, key.Name)
	}
	accessor, _ := meta.Accessor(obj)
	log.FromContext(ctx).Info("strategy.Get",
		"key", key,
		"managedFieldsCount", len(accessor.GetManagedFields()),
		"labelsCount", len(accessor.GetLabels()),
		"finalizersCount", len(accessor.GetFinalizers()),
	)
	return obj, nil
}

func (r *strategy) BeginCreate(ctx context.Context) error { return nil }

func (r *strategy) PrepareForCreate(ctx context.Context, obj runtime.Object) {
	r.obj.PrepareForCreate(ctx, obj)
}

func (r *strategy) Validate(ctx context.Context, obj runtime.Object) field.ErrorList {
	return r.obj.ValidateCreate(ctx, obj)
}

func (r *strategy) InvokeCreate(ctx context.Context, obj runtime.Object, recursion bool) (runtime.Object, error) {
	return obj, nil
}

func (r *strategy) Create(ctx context.Context, key types.NamespacedName, obj runtime.Object, dryrun bool) (runtime.Object, error) {
	b, err := yaml.Marshal(&obj)
	if err == nil {
		log.FromContext(ctx).Info("update strategy", "key", key, "data", string(b))
	}
	
	if dryrun {
		if r.opts != nil && r.opts.DryRunCreateFn != nil {
			return r.opts.DryRunCreateFn(ctx, key, obj, dryrun)
		}
		return obj, nil
	}
	if err := r.storage.Create(ctx, storebackend.KeyFromNSN(key), obj); err != nil {
		return obj, apierrors.NewInternalError(err)
	}
	r.notifyWatcher(ctx, watch.Event{
		Type:   watch.Added,
		Object: obj,
	})
	return obj, nil
}

func (r *strategy) WarningsOnCreate(ctx context.Context, obj runtime.Object) []string {
	return nil
}

func (r *strategy) BeginUpdate(ctx context.Context) error { return nil }

func (r *strategy) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
	r.obj.PrepareForUpdate(ctx, obj, old)
}

func (r *strategy) AllowCreateOnUpdate() bool { return true }

func (r *strategy) AllowUnconditionalUpdate() bool { return false }

func (r *strategy) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	return r.obj.ValidateUpdate(ctx, obj, old)
}

func (r *strategy) InvokeUpdate(ctx context.Context, obj, old runtime.Object, recursion bool) (runtime.Object, runtime.Object, error) {
	return obj, old, nil
}

func (r *strategy) Update(ctx context.Context, key types.NamespacedName, obj, old runtime.Object, dryrun bool) (runtime.Object, error) {

	objNew, err := yaml.Marshal(&obj)
	if err == nil {
		log.FromContext(ctx).Info("update strategy", "key", key, "data", string(objNew))
	}

	objOld, err := yaml.Marshal(&old)
	if err == nil {
		log.FromContext(ctx).Info("update strategy", "key", key, "data", string(objOld))
	}


	if r.obj.IsEqual(ctx, obj, old) {
		return obj, nil
	}

	if dryrun {
		if r.opts != nil && r.opts.DryRunUpdateFn != nil {
			return r.opts.DryRunUpdateFn(ctx, key, obj, old, dryrun)
		}
		return obj, nil
	}

	if err := utils.UpdateResourceVersionAndGeneration(obj, old); err != nil {
		return obj, apierrors.NewInternalError(err)
	}

	if err := r.storage.Update(ctx, storebackend.KeyFromNSN(key), obj); err != nil {
		return obj, apierrors.NewInternalError(err)
	}
	r.notifyWatcher(ctx, watch.Event{
		Type:   watch.Modified,
		Object: obj,
	})
	return obj, nil
}

func (r *strategy) WarningsOnUpdate(ctx context.Context, obj, old runtime.Object) []string {
	return nil
}

func (r *strategy) BeginDelete(ctx context.Context) error {
	log := log.FromContext(ctx)
	log.Debug("BeginDelete strategy")
	return nil
}

func (r *strategy) InvokeDelete(ctx context.Context, obj runtime.Object, recursion bool) (runtime.Object, error) {
	return obj, nil
}

func (r *strategy) Delete(ctx context.Context, key types.NamespacedName, obj runtime.Object, dryrun bool) (runtime.Object, error) {
	log := log.FromContext(ctx)
	log.Debug("Delete strategy", "key", key, "obj", obj, "dryrun", dryrun)
	if dryrun {
		if r.opts != nil && r.opts.DryRunDeleteFn != nil {
			return r.opts.DryRunDeleteFn(ctx, key, obj, dryrun)
		}
		return obj, nil
	}

	if err := r.storage.Delete(ctx, storebackend.KeyFromNSN(key)); err != nil {
		return obj, apierrors.NewInternalError(err)
	}
	r.notifyWatcher(ctx, watch.Event{
		Type:   watch.Deleted,
		Object: obj,
	})
	return obj, nil
}

func (r *strategy) List(ctx context.Context, options *metainternalversion.ListOptions) (runtime.Object, error) {
	log := log.FromContext(ctx)

	var filter resource.Filter
	var err error
	if r.obj.FieldSelector() != nil {
		filter, err = r.obj.FieldSelector()(ctx, options.FieldSelector)
		if err != nil {
			return nil, err
		}
	} else {
		filter, err = utils.ParseFieldSelector(ctx, options.FieldSelector)
		if err != nil {
			return nil, err
		}
	}

	newListObj := r.obj.NewList()
	v, err := utils.GetListPrt(newListObj)
	if err != nil {
		return nil, err
	}

	listFunc := func(ctx context.Context, key storebackend.Key, obj runtime.Object) {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			log.Error("cannot get meta from object", "error", err.Error())
			return
		}

		// Always enforce namespace scoping for namespace-scoped resources
		if r.obj.NamespaceScoped() {
			ns, ok := genericapirequest.NamespaceFrom(ctx)
			if ok && ns != "" && accessor.GetNamespace() != ns {
				return
			}
		}

		if options.LabelSelector != nil || filter != nil {
			f := true
			if options.LabelSelector != nil {
				if options.LabelSelector.Matches(labels.Set(accessor.GetLabels())) {
					f = false
				}
			} else {
				// if no labels selector is present don't filter
				f = false
			}
			// if filtered we dont have to run this section since the label requirement was not met
			if filter != nil && !f {
				f = filter.Filter(ctx, obj)
			}

			if !f {
				utils.AppendItem(v, obj)
			}
		} else {
			utils.AppendItem(v, obj)
		}
	}

	if err := r.storage.List(ctx, listFunc); err != nil {
		log.Error("list failed", "err", err)
		return nil, err
	}
	return newListObj, nil
}

func (r *strategy) BeginWatch(ctx context.Context) error { return nil }

func (r *strategy) Watch(ctx context.Context, options *metainternalversion.ListOptions) (watch.Interface, error) {
	ctx, cancel := context.WithCancel(ctx)

	w := &watcher{
		cancel:         cancel,
		resultChan:     make(chan watch.Event),
		watcherManager: r.watcherManager,
		obj:            r.obj,
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

func (r *strategy) notifyWatcher(ctx context.Context, event watch.Event) {
	log := log.FromContext(ctx).With("eventType", event.Type)
	log.Debug("notify watcherManager")

	r.watcherManager.WatchChan() <- event
}
