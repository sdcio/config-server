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
	"fmt"

	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	"github.com/henderiw/apiserver-builder/pkg/builder/utils"
	"github.com/henderiw/apiserver-store/pkg/rest"
	"github.com/henderiw/apiserver-store/pkg/storebackend"
	watchermanager "github.com/henderiw/apiserver-store/pkg/watcher-manager"
	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/sdcio/config-server/pkg/registry/options"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/storage/names"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
		client:         opts.Client,
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

func (r *strategy) InvokeCreate(ctx context.Context, obj runtime.Object, recursion bool) (runtime.Object, error) {
	return obj, nil
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

func (r *strategy) InvokeUpdate(ctx context.Context, obj, old runtime.Object, recursion bool) (runtime.Object, runtime.Object, error) {
	return obj, old, nil
}

func (r *strategy) Update(ctx context.Context, key types.NamespacedName, obj, old runtime.Object, dryrun bool) (runtime.Object, error) {
	return obj, apierrors.NewMethodNotSupported(r.obj.GetGroupVersionResource().GroupResource(), "update")
}

func (r *strategy) WarningsOnUpdate(ctx context.Context, obj, old runtime.Object) []string {
	return nil
}

func (r *strategy) BeginDelete(ctx context.Context) error { return nil }

func (r *strategy) InvokeDelete(ctx context.Context, obj runtime.Object, recursion bool) (runtime.Object, error) {
	return obj, nil
}

func (r *strategy) Delete(ctx context.Context, key types.NamespacedName, obj runtime.Object, dryrun bool) (runtime.Object, error) {
	return obj, nil
}

func (r *strategy) getRunningConfig(ctx context.Context, target *configv1alpha1.Target, key types.NamespacedName) (*config.RunningConfig, error) {
	if !target.IsReady() {
		return nil, apierrors.NewNotFound(r.gr, key.Name)
	}

	cfg := &dsclient.Config{
		Address:  dsclient.GetDataServerAddress(),
		Insecure: true,
	}

	dsclient, closeFn, err := dsclient.NewEphemeral(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := closeFn(); err != nil {
			// You can use your preferred logging framework here
			fmt.Printf("failed to close connection: %v\n", err)
		}
	}()

	// check if the schema exists; this is == nil check; in case of err it does not exist
	rsp, err := dsclient.GetIntent(ctx, &sdcpb.GetIntentRequest{
		DatastoreName: storebackend.KeyFromNSN(key).String(),
		Intent:        "running",
		Format:        sdcpb.Format_Intent_Format_JSON,
	})
	if err != nil {
		return nil, err
	}

	rc := config.BuildRunningConfig(
		metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		config.RunningConfigSpec{},
		config.RunningConfigStatus{
			Value: runtime.RawExtension{
				Raw: rsp.GetBlob(),
			},
		},
	)

	rc.SetCreationTimestamp(target.CreationTimestamp)
	rc.SetResourceVersion(target.ResourceVersion)
	rc.SetAnnotations(target.Annotations)
	rc.SetLabels(target.Labels)
	return rc, nil
}

func (r *strategy) Get(ctx context.Context, key types.NamespacedName) (runtime.Object, error) {
	target := &configv1alpha1.Target{}
	if err := r.client.Get(ctx, key, target); err != nil {
		return nil, apierrors.NewNotFound(r.gr, key.Name)
	}
	return r.getRunningConfig(ctx, target, key)
}

func (r *strategy) List(ctx context.Context, opts *metainternalversion.ListOptions) (runtime.Object, error) {
	log := log.FromContext(ctx)

	filter, err := options.ParseFieldSelector(ctx, opts.FieldSelector)
	if err != nil {
		return nil, err
	}

	newListObj := r.obj.NewList()
	v, err := utils.GetListPrt(newListObj)
	if err != nil {
		return nil, err
	}

	targets := &configv1alpha1.TargetList{}
	if err := r.client.List(ctx, targets, options.ListOptsFromInternal(filter, opts)...); err != nil {
		return nil, err
	}

	for _, target := range targets.Items {
		key := target.GetNamespacedName()
		obj, err := r.getRunningConfig(ctx, &target, key)
		if err != nil {
			log.Error("cannot get configblame", "key", key.String(), "error", err.Error())
			continue
		}
		utils.AppendItem(v, obj)
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
