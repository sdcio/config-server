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

	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	configv1alpha1 "github.com/iptecharch/config-server/apis/config/v1alpha1"
	"github.com/iptecharch/config-server/pkg/store"
	"github.com/iptecharch/config-server/pkg/target"
	watchermanager "github.com/iptecharch/config-server/pkg/watcher-manager"
	"go.opentelemetry.io/otel/trace"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	builderrest "github.com/henderiw/apiserver-builder/pkg/builder/rest"
)

func NewRunningConfigProviderHandler(ctx context.Context, p rest.Storage) builderrest.ResourceHandlerProvider {
	return func(ctx context.Context, scheme *runtime.Scheme, getter generic.RESTOptionsGetter) (rest.Storage, error) {
		return p, nil
	}
}

func NewRunningConfigProvider(
	ctx context.Context,
	obj resource.Object,
	scheme *runtime.Scheme,
	client client.Client,
	targetStore store.Storer[target.Context]) (rest.Storage, error) {

	return newRunningConfigProvider(ctx, obj, client, targetStore)
}

func newRunningConfigProvider(
	ctx context.Context,
	obj resource.Object,
	client client.Client,
	targetStore store.Storer[target.Context]) (rest.Storage, error) {
	// initialie the rest storage object
	gr := obj.GetGroupVersionResource().GroupResource()
	c := &runningConfig{
		configCommon: configCommon{
			client:         client,
			targetStore:    targetStore,
			gr:             gr,
			isNamespaced:   obj.NamespaceScoped(),
			newFunc:        obj.New,
			newListFunc:    obj.NewList,
			watcherManager: watchermanager.New(32),
		},
		TableConvertor: NewRunningConfigTableConvertor(gr),
	}
	go c.configCommon.watcherManager.Start(ctx)

	return c, nil
}

var _ rest.Lister = &runningConfig{}
var _ rest.Getter = &runningConfig{}
var _ rest.Scoper = &runningConfig{}
var _ rest.Storage = &runningConfig{}
var _ rest.TableConvertor = &runningConfig{}
var _ rest.SingularNameProvider = &runningConfig{}

type runningConfig struct {
	configCommon
	rest.TableConvertor
}

func (r *runningConfig) Destroy() {}

func (r *runningConfig) New() runtime.Object {
	return r.newFunc()
}

func (r *runningConfig) NewList() runtime.Object {
	return r.newListFunc()
}

func (r *runningConfig) NamespaceScoped() bool {
	return true
}

func (r *runningConfig) GetSingularName() string {
	return "runningconfig"
}

func (r *runningConfig) Get(
	ctx context.Context,
	name string,
	options *metav1.GetOptions,
) (runtime.Object, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "runningconfigs::Get", trace.WithAttributes())
	defer span.End()

	options.TypeMeta = metav1.TypeMeta{
		APIVersion: configv1alpha1.SchemeBuilder.GroupVersion.Identifier(),
		Kind:       configv1alpha1.RunningConfigKind,
	}

	return r.get(ctx, name, options)
}

func (r *runningConfig) List(
	ctx context.Context,
	options *metainternalversion.ListOptions,
) (runtime.Object, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "runningconfigs::List", trace.WithAttributes())
	defer span.End()

	options.TypeMeta = metav1.TypeMeta{
		APIVersion: configv1alpha1.SchemeBuilder.GroupVersion.Identifier(), 
		Kind: configv1alpha1.RunningConfigKind,
	}

	return r.list(ctx, options)
}

func (r *runningConfig) Watch(
	ctx context.Context,
	options *metainternalversion.ListOptions,
) (watch.Interface, error) {
	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configs::Watch", trace.WithAttributes())
	defer span.End()

	options.TypeMeta = metav1.TypeMeta{
		APIVersion: configv1alpha1.SchemeBuilder.GroupVersion.Identifier(), 
		Kind: configv1alpha1.RunningConfigKind}

	w := r.watch(ctx, options)

	return w, nil
}
