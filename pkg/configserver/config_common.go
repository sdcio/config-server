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
	"fmt"

	"github.com/henderiw/logger/log"
	configv1alpha1 "github.com/iptecharch/config-server/apis/config/v1alpha1"
	"github.com/iptecharch/config-server/pkg/store"
	"github.com/iptecharch/config-server/pkg/target"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	OptimisticLockErrorMsg = "the object has been modified; please apply your changes to the latest version and try again"
)

type configCommon struct {
	client         client.Client
	configStore    store.Storer[runtime.Object]
	configSetStore store.Storer[runtime.Object]
	targetStore    store.Storer[target.Context]
	gr             schema.GroupResource
	isNamespaced   bool
	newFunc        func() runtime.Object
	newListFunc    func() runtime.Object
}

func (r *configCommon) get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	// Get Key
	key, err := r.getKey(ctx, name)
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	log := log.FromContext(ctx).With("key", key.String(), "kind", options.Kind)
	log.Info("get...")

	// get the data from the store
	var obj runtime.Object
	switch options.Kind {
	case configv1alpha1.ConfigKind:
		obj, err = r.configStore.Get(ctx, key)
		if err != nil {
			return nil, apierrors.NewNotFound(r.gr, name)
		}
	case configv1alpha1.ConfigSetKind:
		obj, err = r.configSetStore.Get(ctx, key)
		if err != nil {
			return nil, apierrors.NewNotFound(r.gr, name)
		}
	default:
		return nil, apierrors.NewBadRequest(fmt.Sprintf("unsupported kind, got: %s", options.Kind))
	}

	log.Info("get succeeded", "obj", obj)
	return obj, nil
}

func (r *configCommon) list(
	ctx context.Context,
	options *metainternalversion.ListOptions,
) (runtime.Object, error) {
	// logger
	log := log.FromContext(ctx).With("kind", options.Kind)
	log.Info("list...")

	// Get Key
	_, namespaced := genericapirequest.NamespaceFrom(ctx)
	if namespaced != r.isNamespaced {
		return nil, fmt.Errorf("namespace mismatch got %t, want %t", namespaced, r.isNamespaced)
	}

	newListObj := r.newListFunc()
	v, err := getListPrt(newListObj)
	if err != nil {
		return nil, err
	}

	listFunc := func(ctx context.Context, key store.Key, obj runtime.Object) {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			log.Error("cannot get meta from object", "error", err.Error())
			return
		}

		filtered := false
		if options.FieldSelector == nil {
			log.Info("list", "fieldselector", "nil")
		} else {
			log.Info("list", "fieldselector", "not nil")
			for _, req := range options.FieldSelector.Requirements() {
				filtered = true
				log.Info("list", "fieldselector", "not nil",
					"Operator", req.Operator,
					"Field", req.Field,
					"Value", req.Value,
				)
				switch req.Operator {
				case selection.Equals:
					if req.Field == "metadata.name" {
						if req.Value == accessor.GetName() {
							appendItem(v, obj)
						}
					}
				}
			}
		}

		if !filtered {
			appendItem(v, obj)
		}
	}

	// get the data from the store
	switch options.Kind {
	case configv1alpha1.ConfigKind:
		r.configStore.List(ctx, listFunc)
	case configv1alpha1.ConfigSetKind:
		r.configSetStore.List(ctx, listFunc)
	default:
		return nil, apierrors.NewBadRequest(fmt.Sprintf("unsupported kind, got: %s", options.Kind))
	}

	return newListObj, nil
}
