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
	"fmt"
	"strconv"

	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	"github.com/henderiw/apiserver-builder/pkg/builder/utils"
	"github.com/henderiw/apiserver-store/pkg/rest"
	"github.com/henderiw/apiserver-store/pkg/storebackend"
	watchermanager "github.com/henderiw/apiserver-store/pkg/watcher-manager"
	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/apis/condition"
	"github.com/sdcio/config-server/pkg/registry/options"
	"k8s.io/apimachinery/pkg/api/equality"
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

type conditionsGetter interface {
	GetConditions() []condition.Condition
}

// NewStrategy creates and returns a strategy instance
func NewStrategy(
	obj resource.InternalObject,
	typer runtime.ObjectTyper,
	storage storebackend.Storer[runtime.Object],
	watcherManager watchermanager.WatcherManager,
	opts *options.Options,
) *strategy {
	gr := obj.GetGroupVersionResource().GroupResource()
	return &strategy{
		ObjectTyper:    typer,
		NameGenerator:  names.SimpleNameGenerator,
		gr:             gr,
		obj:            obj,
		storage:        storage,
		watcherManager: watcherManager,
		opts:           opts,
		// shared with every other strategy of this GroupResource (e.g. status)
		rv: allocatorFor(gr),
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
	rv             *rvAllocator
}

func (r *strategy) NamespaceScoped() bool { return r.obj.NamespaceScoped() }

func (r *strategy) Canonicalize(obj runtime.Object) {}

// initRV makes sure the resourceVersion counter starts above every stored object.
func (r *strategy) initRV(ctx context.Context) {
	r.rv.ensureInit(ctx, func(visit func(runtime.Object)) error {
		return r.storage.List(ctx, func(_ context.Context, _ storebackend.Key, obj runtime.Object) {
			visit(obj)
		})
	})
}

func (r *strategy) Get(ctx context.Context, key types.NamespacedName) (runtime.Object, error) {
	obj, err := r.storage.Get(ctx, storebackend.KeyFromNSN(key))
	if err != nil {
		return nil, apierrors.NewNotFound(r.gr, key.Name)
	}
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
	if shouldLog(obj) {
		logObject(ctx, "Create", key, obj)
	}

	if dryrun {
		if r.opts != nil && r.opts.DryRunCreateFn != nil {
			return r.opts.DryRunCreateFn(ctx, key, obj, dryrun)
		}
		return obj, nil
	}

	r.initRV(ctx)
	err := r.rv.write(func(rv string) (*watch.Event, error) {
		// replaces the "0" set by Store.Create
		if err := setResourceVersion(obj, rv); err != nil {
			return nil, apierrors.NewInternalError(err)
		}
		if err := r.storage.Create(ctx, storebackend.KeyFromNSN(key), obj); err != nil {
			return nil, apierrors.NewInternalError(err)
		}
		return &watch.Event{Type: watch.Added, Object: obj}, nil
	}, r.publish(ctx))
	return obj, err
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
	if shouldLog(obj) {
		logObject(ctx, "Update New", key, obj)
		logObject(ctx, "Update Old", key, old)
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

	// BeforeUpdate already reset generation to the old value; bump it only
	// when the spec changed or deletion started. The resourceVersion is set
	// below from the collection-wide counter.
	if err := setNextGeneration(obj, old); err != nil {
		return obj, apierrors.NewInternalError(err)
	}

	r.initRV(ctx)
	err := r.rv.write(func(rv string) (*watch.Event, error) {
		if err := setResourceVersion(obj, rv); err != nil {
			return nil, apierrors.NewInternalError(err)
		}
		if err := r.storage.Update(ctx, storebackend.KeyFromNSN(key), obj); err != nil {
			return nil, apierrors.NewInternalError(err)
		}
		return &watch.Event{Type: watch.Modified, Object: obj}, nil
	}, r.publish(ctx))
	return obj, err
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

	r.initRV(ctx)
	// a delete is a write: the Deleted event carries a new resourceVersion
	deleted := obj.DeepCopyObject()
	err := r.rv.write(func(rv string) (*watch.Event, error) {
		if err := r.storage.Delete(ctx, storebackend.KeyFromNSN(key)); err != nil {
			return nil, apierrors.NewInternalError(err)
		}
		if err := setResourceVersion(deleted, rv); err != nil {
			return nil, apierrors.NewInternalError(err)
		}
		return &watch.Event{Type: watch.Deleted, Object: deleted}, nil
	}, r.publish(ctx))
	if err != nil {
		return obj, err
	}
	return deleted, nil
}

// setNextGeneration applies the Kubernetes generation rule: generation
// changes only when the spec changes or when deletionTimestamp is set.
// Metadata (labels, annotations, finalizers, ownerReferences) and status
// changes leave it untouched, so GenerationChangedPredicate and
// observedGeneration keep their meaning.
func setNextGeneration(obj, old runtime.Object) error {
	gen, err := nextGeneration(obj, old)
	if err != nil {
		return err
	}
	acc, err := meta.Accessor(obj)
	if err != nil {
		return err
	}
	acc.SetGeneration(gen)
	return nil
}

func nextGeneration(obj, old runtime.Object) (int64, error) {
	newAcc, err := meta.Accessor(obj)
	if err != nil {
		return 0, err
	}
	oldAcc, err := meta.Accessor(old)
	if err != nil {
		return 0, err
	}
	gen := oldAcc.GetGeneration()

	if oldAcc.GetDeletionTimestamp() == nil && newAcc.GetDeletionTimestamp() != nil {
		return gen + 1, nil
	}

	newU, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return 0, err
	}
	oldU, err := runtime.DefaultUnstructuredConverter.ToUnstructured(old)
	if err != nil {
		return 0, err
	}
	if !equality.Semantic.DeepEqual(newU["spec"], oldU["spec"]) {
		return gen + 1, nil
	}
	return gen, nil
}

// predicate returns the filter shared by List and Watch: namespace scoping,
// label selector and field selector. It returns true if obj is selected.
func (r *strategy) predicate(ctx context.Context, options *metainternalversion.ListOptions) (func(context.Context, runtime.Object) bool, error) {
	var filter resource.Filter
	var err error
	var fieldSelector = options.FieldSelector
	if r.obj.FieldSelector() != nil {
		filter, err = r.obj.FieldSelector()(ctx, fieldSelector)
	} else {
		filter, err = utils.ParseFieldSelector(ctx, fieldSelector)
	}
	if err != nil {
		return nil, err
	}

	var labelSelector labels.Selector = options.LabelSelector
	ns, hasNS := genericapirequest.NamespaceFrom(ctx)
	scoped := r.obj.NamespaceScoped()

	return func(ctx context.Context, obj runtime.Object) bool {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			log.FromContext(ctx).Error("cannot get meta from object", "error", err.Error())
			return false
		}
		// Always enforce namespace scoping for namespace-scoped resources
		if scoped && hasNS && ns != "" && accessor.GetNamespace() != ns {
			return false
		}
		if labelSelector != nil && !labelSelector.Matches(labels.Set(accessor.GetLabels())) {
			return false
		}
		// Filter returns true when the object is filtered OUT
		if filter != nil && filter.Filter(ctx, obj) {
			return false
		}
		return true
	}, nil
}

func (r *strategy) List(ctx context.Context, options *metainternalversion.ListOptions) (runtime.Object, error) {
	log := log.FromContext(ctx)

	selected, err := r.predicate(ctx, options)
	if err != nil {
		return nil, err
	}

	newListObj := r.obj.NewList()
	v, err := utils.GetListPrt(newListObj)
	if err != nil {
		return nil, err
	}

	// Read the resourceVersion BEFORE iterating: every write up to it is in
	// storage. A concurrent later write may also show up in the list; the
	// client then sees it once more on the watch, which is harmless.
	r.initRV(ctx)
	listRV := r.rv.Current()

	if err := r.storage.List(ctx, func(ctx context.Context, key storebackend.Key, obj runtime.Object) {
		if selected(ctx, obj) {
			utils.AppendItem(v, obj)
		}
	}); err != nil {
		log.Error("list failed", "err", err)
		return nil, err
	}

	listAcc, err := meta.ListAccessor(newListObj)
	if err != nil {
		return nil, apierrors.NewInternalError(err)
	}
	listAcc.SetResourceVersion(strconv.FormatUint(listRV, 10))
	return newListObj, nil
}

func (r *strategy) BeginWatch(ctx context.Context) error { return nil }

func (r *strategy) Watch(ctx context.Context, options *metainternalversion.ListOptions) (watch.Interface, error) {
	ctx, cancel := context.WithCancel(ctx)

	w := newWatcher(cancel, r.watcherManager)
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

// publish returns the function that hands an event to the watcherManager.
// It is called with the allocator lock held, so events are ordered by
// resourceVersion. Watcher callbacks never block (see watcher.OnChange).
func (r *strategy) publish(ctx context.Context) func(watch.Event) {
	return func(event watch.Event) {
		log.FromContext(ctx).With("eventType", event.Type).Debug("notify watcherManager")
		r.watcherManager.WatchChan() <- event
	}
}

func shouldLog(obj runtime.Object) bool {
	return true
	//return obj.GetObjectKind().GroupVersionKind().Kind == "Target"
}

func logObject(ctx context.Context, op string, key types.NamespacedName, obj runtime.Object) {
	accessor, _ := meta.Accessor(obj)
	l := log.FromContext(ctx).With(
		"op", op,
		"key", key,
		"kind", obj.GetObjectKind().GroupVersionKind().Kind,
		"resourceVersion", accessor.GetResourceVersion(),
		"generation", accessor.GetGeneration(),
		"managedFields", len(accessor.GetManagedFields()),
		"finalizers", accessor.GetFinalizers(),
	)
	if cg, ok := obj.(conditionsGetter); ok {
		conditions := cg.GetConditions()
		condSummary := make([]string, 0, len(conditions))
		for _, c := range conditions {
			condSummary = append(condSummary, fmt.Sprintf("%s=%s(%s)", c.Type, c.Status, c.Message))
		}
		l = l.With("conditions", condSummary)
	}
	l.Info("strategy")
}