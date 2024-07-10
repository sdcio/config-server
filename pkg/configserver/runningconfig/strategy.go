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
/*
import (
	"context"
	"fmt"

	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	"github.com/henderiw/apiserver-store/pkg/rest"
	"github.com/henderiw/apiserver-store/pkg/storebackend"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/sdcio/config-server/pkg/target"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/names"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewStrategy creates and returns a fischerStrategy instance
func NewStrategy(ctx context.Context, typer runtime.ObjectTyper, client client.Client, targetStore storebackend.Storer[*target.Context]) *strategy {
	//watcherManager := watchermanager.New(32)

	//go watcherManager.Start(ctx)

	return &strategy{
		ObjectTyper:   typer,
		NameGenerator: names.SimpleNameGenerator,
		client:        client,
		targetStore:   targetStore,
		gr:            configv1alpha1.Resource(configv1alpha1.RunningConfigPlural),
		resource:      &configv1alpha1.RunningConfig{},
		//watcherManager: watcherManager,
	}
}

// MatchPackageRevision is the filter used by the generic etcd backend to watch events
// from etcd to clients of the apiserver only interested in specific labels/fields.
func Match(label labels.Selector, field fields.Selector) storage.SelectionPredicate {
	return storage.SelectionPredicate{
		Label:    label,
		Field:    field,
		GetAttrs: GetAttrs,
	}
}

// GetAttrs returns labels.Set, fields.Set, and error in case the given runtime.Object does not match
func GetAttrs(obj runtime.Object) (labels.Set, fields.Set, error) {
	api, ok := obj.(*configv1alpha1.RunningConfig)
	if !ok {
		return nil, nil, fmt.Errorf("given object is not a %s", configv1alpha1.RunningConfigKind)
	}
	return labels.Set(api.ObjectMeta.Labels), SelectableFields(api), nil
}

// SelectableFields returns a field set that represents the object.
func SelectableFields(obj *configv1alpha1.RunningConfig) fields.Set {
	return generic.ObjectMetaFieldsSet(&obj.ObjectMeta, true)
}

var _ rest.RESTGetStrategy = &strategy{}
var _ rest.RESTListStrategy = &strategy{}
var _ rest.RESTCreateStrategy = &strategy{}
var _ rest.RESTUpdateStrategy = &strategy{}
var _ rest.RESTDeleteStrategy = &strategy{}
var _ rest.RESTWatchStrategy = &strategy{}

type strategy struct {
	runtime.ObjectTyper
	names.NameGenerator
	client      client.Client
	targetStore storebackend.Storer[*target.Context]
	gr          schema.GroupResource
	resource    resource.Object
	//watcherManager watchermanager.WatcherManager
}

func (r *strategy) NamespaceScoped() bool {
	return true
}

func (r *strategy) Canonicalize(obj runtime.Object) {}
*/