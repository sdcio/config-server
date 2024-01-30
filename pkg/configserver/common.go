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
	"errors"
	"fmt"

	"github.com/henderiw/logger/log"
	configv1alpha1 "github.com/iptecharch/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/store"
	"github.com/iptecharch/config-server/pkg/target"
	watchermanager "github.com/iptecharch/config-server/pkg/watcher-manager"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
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
	watcherManager watchermanager.WatcherManager
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
	case configv1alpha1.RunningConfigKind:
		target, tctx, err := r.getTargetRunningContext(ctx, key)
		if err != nil {
			return nil, apierrors.NewNotFound(r.gr, name)
		}
		rc, err := tctx.GetData(ctx, key)
		if err != nil {
			return nil, apierrors.NewInternalError(err)
		}
		rc.SetCreationTimestamp(target.CreationTimestamp)
		rc.SetResourceVersion(target.ResourceVersion)
		rc.SetAnnotations(target.Annotations)
		rc.SetLabels(target.Labels)
		obj = rc
	default:
		return nil, apierrors.NewBadRequest(fmt.Sprintf("unsupported kind, got: %s", options.Kind))
	}

	log.Info("get succeeded")
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

	configFilter, err := parseFieldSelector(options.FieldSelector)
	if err != nil {
		return nil, err
	}

	newListObj := r.newListFunc()
	v, err := getListPrt(newListObj)
	if err != nil {
		return nil, err
	}

	configListFunc := func(ctx context.Context, key store.Key, obj runtime.Object) {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			log.Error("cannot get meta from object", "error", err.Error())
			return
		}

		if options.LabelSelector != nil || configFilter != nil {
			filter := true
			if options.LabelSelector != nil {
				if options.LabelSelector.Matches(labels.Set(accessor.GetLabels())) {
					filter = false
				}
			} else {
				// if not labels selector is present don't filter
				filter = false
			}
			// if filtered we dont have to run this section since the label requirement was not met
			if configFilter != nil && !filter {
				if configFilter.Name != "" {
					if accessor.GetName() == configFilter.Name {
						filter = false
					} else {
						filter = true
					}
				}
				if configFilter.Namespace != "" {
					if accessor.GetNamespace() == configFilter.Namespace {
						filter = false
					} else {
						filter = true
					}
				}
			}
			if !filter {
				appendItem(v, obj)
			}
		} else {
			appendItem(v, obj)
		}
	}

	runningConfigListFunc := func(ctx context.Context, key store.Key, tctx target.Context) {
		target := &invv1alpha1.Target{}
		if err := r.client.Get(ctx, key.NamespacedName, target); err != nil {
			log.Error("cannot get target", "key", key.String(), "error", err.Error())
			return
		}

		if options.LabelSelector != nil || configFilter != nil {
			filter := true
			if options.LabelSelector != nil {
				if options.LabelSelector.Matches(labels.Set(target.GetLabels())) {
					filter = false
				}
			} else {
				// if not labels selector is present don't filter
				filter = false
			}
			// if filtered we dont have to run this section since the label requirement was not met
			if configFilter != nil && !filter {
				if configFilter.Name != "" {
					if target.GetName() == configFilter.Name {
						filter = false
					} else {
						filter = true
					}
				}
				if configFilter.Namespace != "" {
					if target.GetNamespace() == configFilter.Namespace {
						filter = false
					} else {
						filter = true
					}
				}
			}
			if !filter {
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

	// get the data from the store
	switch options.Kind {
	case configv1alpha1.ConfigKind:
		r.configStore.List(ctx, configListFunc)
	case configv1alpha1.ConfigSetKind:
		r.configSetStore.List(ctx, configListFunc)
	case configv1alpha1.RunningConfigKind:
		r.targetStore.List(ctx, runningConfigListFunc)
	default:
		return nil, apierrors.NewBadRequest(fmt.Sprintf("unsupported kind, got: %s", options.Kind))
	}

	return newListObj, nil
}

func (r *configCommon) watch(
	ctx context.Context,
	options *metainternalversion.ListOptions,
) *watcher {

	// logger
	log := log.FromContext(ctx)

	if options.FieldSelector == nil {
		log.Info("watch", "options", *options, "fieldselector", "nil")
	} else {
		requirements := options.FieldSelector.Requirements()
		log.Info("watch", "options", *options, "fieldselector", options.FieldSelector.Requirements())
		for _, requirement := range requirements {
			log.Info("watch requirement",
				"Operator", requirement.Operator,
				"Value", requirement.Value,
				"Field", requirement.Field,
			)
		}
	}

	ctx, cancel := context.WithCancel(ctx)

	w := &watcher{
		cancel:         cancel,
		resultChan:     make(chan watch.Event),
		watcherManager: r.watcherManager,
	}

	go w.listAndWatch(ctx, r, options)

	return w
}

func (r *configCommon) notifyWatcher(ctx context.Context, event watch.Event) {
	log := log.FromContext(ctx).With("eventType", event.Type)
	log.Info("notify watcherManager")

	r.watcherManager.WatchChan() <- event
}

type lister interface {
	list(ctx context.Context, options *metainternalversion.ListOptions) (runtime.Object, error)
}

func (r *configCommon) getTargetContext(ctx context.Context, targetKey store.Key) (*target.Context, error) {
	target := &invv1alpha1.Target{}
	if err := r.client.Get(ctx, targetKey.NamespacedName, target); err != nil {
		return nil, err
	}
	if !target.IsConfigReady() {
		return nil, errors.New(string(configv1alpha1.ConditionReasonTargetNotReady))
	}
	tctx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil {
		return nil, errors.New(string(configv1alpha1.ConditionReasonTargetNotFound))
	}
	return &tctx, nil
}

func (r *configCommon) getTargetRunningContext(ctx context.Context, targetKey store.Key) (*invv1alpha1.Target, *target.Context, error) {
	target := &invv1alpha1.Target{}
	if err := r.client.Get(ctx, targetKey.NamespacedName, target); err != nil {
		return nil, nil, apierrors.NewNotFound(r.gr, targetKey.Name)
	}
	if !target.DeletionTimestamp.IsZero() {
		return nil, nil, apierrors.NewNotFound(r.gr, targetKey.Name)
	}
	if !target.IsReady() {
		return nil, nil, apierrors.NewInternalError(fmt.Errorf("target not ready"))
	}
	tctx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil {
		return nil, nil, apierrors.NewNotFound(r.gr, targetKey.Name)
	}
	return target, &tctx, nil
}
