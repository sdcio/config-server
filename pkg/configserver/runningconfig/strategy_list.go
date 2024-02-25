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
	"reflect"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/target"
	"k8s.io/apimachinery/pkg/api/meta"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
)

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

	newListObj := r.resource.NewList()
	v, err := getListPrt(newListObj)
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
				appendItem(v, obj)
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
			appendItem(v, obj)
		}
	}

	r.targetStore.List(ctx, runningConfigListFunc)
	return newListObj, nil
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
