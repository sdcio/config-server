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

package unmanagedconfig

import (
	"context"
	"fmt"
	"reflect"

	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	"github.com/henderiw/apiserver-store/pkg/rest"
	"github.com/henderiw/apiserver-store/pkg/storebackend"
	watchermanager "github.com/henderiw/apiserver-store/pkg/watcher-manager"
	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/apis/config"
	"github.com/sdcio/config-server/pkg/registry/options"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/storage/names"
	"sigs.k8s.io/structured-merge-diff/v4/fieldpath"
)

// NewStatusStrategy creates and returns a sttaus strategy instance
func NewStatusStrategy(
	obj resource.InternalObject,
	typer runtime.ObjectTyper,
	storage storebackend.Storer[runtime.Object],
	watcherManager watchermanager.WatcherManager,
	opts *options.Options,
) *statusStrategy {

	return &statusStrategy{
		ObjectTyper:    typer,
		NameGenerator:  names.SimpleNameGenerator,
		gr:             obj.GetGroupVersionResource().GroupResource(),
		obj:            obj,
		storage:        storage,
		watcherManager: watcherManager,
		opts:           opts,
	}
}

var _ rest.RESTUpdateStrategy = &statusStrategy{}
var _ rest.ResetFieldsStrategy = &statusStrategy{}

type statusStrategy struct {
	runtime.ObjectTyper
	names.NameGenerator
	gr             schema.GroupResource
	obj            resource.Object
	storage        storebackend.Storer[runtime.Object]
	watcherManager watchermanager.WatcherManager
	opts           *options.Options
}

func (r *statusStrategy) NamespaceScoped() bool { return r.obj.NamespaceScoped() }

func (r *statusStrategy) Canonicalize(obj runtime.Object) {}

func (r *statusStrategy) AllowCreateOnUpdate() bool { return false }

func (r *statusStrategy) AllowUnconditionalUpdate() bool { return false }

func (r *statusStrategy) BeginUpdate(ctx context.Context) error { return nil }

func (r *statusStrategy) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
	newObj := obj.(*config.UnManagedConfig)
	oldObj := old.(*config.UnManagedConfig)
	newObj.Spec = oldObj.Spec

	// Status updates are for only for updating status, not objectmeta.
	metav1.ResetObjectMetaForStatus(&newObj.ObjectMeta, &newObj.ObjectMeta)
}

func (r *statusStrategy) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	var allErrs field.ErrorList
	return allErrs
}

func (r *statusStrategy) Update(ctx context.Context, key types.NamespacedName, obj, old runtime.Object, dryrun bool) (runtime.Object, error) {
	log := log.FromContext(ctx)
	// check if there is a change
	newConfig, ok := obj.(*config.UnManagedConfig)
	if !ok {
		return obj, fmt.Errorf("unexpected new object, expecting: %s, got: %s", config.ConfigKind, reflect.TypeOf(obj))
	}
	oldConfig, ok := old.(*config.UnManagedConfig)
	if !ok {
		return obj, fmt.Errorf("unexpected old object, expecting: %s, got: %s", config.ConfigKind, reflect.TypeOf(obj))
	}

	if apiequality.Semantic.DeepEqual(oldConfig.Status, newConfig.Status) {
		log.Debug("update nothing to do")
		return obj, nil
	}

	if dryrun {
		return obj, nil
	}

	if err := updateResourceVersion(ctx, obj, old); err != nil {
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

func (r *statusStrategy) WarningsOnUpdate(ctx context.Context, obj, old runtime.Object) []string {
	return nil
}

// GetResetFields returns the set of fields that get reset by the strategy
// and should not be modified by the user.
func (statusStrategy) GetResetFields() map[fieldpath.APIVersion]*fieldpath.Set {
	fields := map[fieldpath.APIVersion]*fieldpath.Set{
		fieldpath.APIVersion(apiextensions.SchemeGroupVersion.Identifier()): fieldpath.NewSet(
			fieldpath.MakePathOrDie("status"),
		),
	}

	return fields
}

func (r *statusStrategy) notifyWatcher(ctx context.Context, event watch.Event) {
	log := log.FromContext(ctx).With("eventType", event.Type)
	log.Info("notify watcherManager")

	r.watcherManager.WatchChan() <- event
}
