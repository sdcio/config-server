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
	"github.com/henderiw/apiserver-store/pkg/rest"
	"github.com/henderiw/apiserver-store/pkg/storebackend"
	watchermanager "github.com/henderiw/apiserver-store/pkg/watcher-manager"
	"github.com/sdcio/config-server/pkg/registry/options"
	logf "github.com/sdcio/logger"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/storage/names"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"
)

// NewStatusStrategy creates and returns a status strategy instance
func NewStatusStrategy(
	obj resource.ObjectWithStatusSubResource,
	typer runtime.ObjectTyper,
	storage storebackend.Storer[runtime.Object],
	watcherManager watchermanager.WatcherManager,
	opts *options.Options,
) *statusStrategy {
	gr := obj.GetGroupVersionResource().GroupResource()
	return &statusStrategy{
		ObjectTyper:    typer,
		NameGenerator:  names.SimpleNameGenerator,
		gr:             gr,
		obj:            obj,
		storage:        storage,
		watcherManager: watcherManager,
		opts:           opts,
		// the SAME allocator as the main strategy of this resource: a status
		// update is a write to the same collection and must take the next
		// collection-wide resourceVersion
		rv: allocatorFor(gr),
	}
}

var _ rest.RESTUpdateStrategy = &statusStrategy{}
var _ rest.ResetFieldsStrategy = &statusStrategy{}

type statusStrategy struct {
	runtime.ObjectTyper
	names.NameGenerator
	gr             schema.GroupResource
	obj            resource.ObjectWithStatusSubResource
	storage        storebackend.Storer[runtime.Object]
	watcherManager watchermanager.WatcherManager
	opts           *options.Options
	rv             *rvAllocator
}

func (r *statusStrategy) NamespaceScoped() bool { return r.obj.NamespaceScoped() }

func (r *statusStrategy) Canonicalize(obj runtime.Object) {}

func (r *statusStrategy) AllowCreateOnUpdate() bool { return false }

func (r *statusStrategy) AllowUnconditionalUpdate() bool { return false }

func (r *statusStrategy) BeginUpdate(ctx context.Context) error { return nil }

func (r *statusStrategy) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
	r.obj.PrepareForStatusUpdate(ctx, obj, old)
}

func (r *statusStrategy) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	return r.obj.ValidateStatusUpdate(ctx, obj, old)
}

func (r *statusStrategy) InvokeUpdate(ctx context.Context, obj, old runtime.Object, recursion bool) (runtime.Object, runtime.Object, error) {
	return obj, old, nil
}

func (r *statusStrategy) Update(ctx context.Context, key types.NamespacedName, obj, old runtime.Object, dryrun bool) (runtime.Object, error) {
	if shouldLog(obj) {
		logObject(ctx, "Update Status New", key, obj)
		logObject(ctx, "Update Status Old", key, old)
	}

	// check if there is a change
	if r.obj.IsStatusEqual(ctx, obj, old) {
		return obj, nil
	}

	if dryrun {
		return obj, nil
	}

	r.rv.ensureInit(ctx, func(visit func(runtime.Object)) error {
		return r.storage.List(ctx, func(_ context.Context, _ storebackend.Key, o runtime.Object) {
			visit(o)
		})
	})

	// Same path as the main strategy: next collection-wide resourceVersion,
	// persist, and publish the event under the allocator lock so status and
	// spec events of this resource reach watchers in resourceVersion order.
	// (A status update does not change the generation.)
	err := r.rv.write(func(rv string) (*watch.Event, error) {
		if err := setResourceVersion(obj, rv); err != nil {
			return nil, apierrors.NewInternalError(err)
		}
		if err := r.storage.Update(ctx, storebackend.KeyFromNSN(key), obj); err != nil {
			return nil, apierrors.NewInternalError(err)
		}
		return &watch.Event{Type: watch.Modified, Object: obj}, nil
	}, func(event watch.Event) {
		logf.FromContext(ctx).WithValues("eventType", event.Type).V(logf.VDebug).Info("notify watcherManager")
		r.watcherManager.WatchChan() <- event
	})
	return obj, err
}

func (r *statusStrategy) WarningsOnUpdate(ctx context.Context, obj, old runtime.Object) []string {
	return nil
}

// GetResetFields returns the set of fields that get reset by the strategy
// and should not be modified by the user.
func (r *statusStrategy) GetResetFields() map[fieldpath.APIVersion]*fieldpath.Set {
	fields := map[fieldpath.APIVersion]*fieldpath.Set{
		fieldpath.APIVersion(r.obj.GetGroupVersionResource().GroupVersion().String()): fieldpath.NewSet(
			fieldpath.MakePathOrDie("status"),
		),
	}
	return fields
}
