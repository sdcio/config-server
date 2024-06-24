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

package eventhandler

import (
	"context"

	"github.com/henderiw/logger/log"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ConfigForConfigSetEventHandler struct {
	Client         client.Client
	ControllerName string
}

// Create enqueues a request
func (r *ConfigForConfigSetEventHandler) Create(ctx context.Context, evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	r.add(ctx, evt.Object, q)
}

// Create enqueues a request
func (r *ConfigForConfigSetEventHandler) Update(ctx context.Context, evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	r.add(ctx, evt.ObjectOld, q)
	r.add(ctx, evt.ObjectNew, q)
}

// Create enqueues a request
func (r *ConfigForConfigSetEventHandler) Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
	r.add(ctx, evt.Object, q)
}

// Create enqueues a request
func (r *ConfigForConfigSetEventHandler) Generic(ctx context.Context, evt event.GenericEvent, q workqueue.RateLimitingInterface) {
	r.add(ctx, evt.Object, q)
}

func (r *ConfigForConfigSetEventHandler) add(ctx context.Context, obj runtime.Object, queue adder) {
	config, ok := obj.(*configv1alpha1.Config)
	if !ok {
		return
	}
	ctx = ctrlconfig.InitContext(ctx, r.ControllerName, types.NamespacedName{Namespace: "config-event", Name: config.GetName()})
	log := log.FromContext(ctx)

	configSetList := &configv1alpha1.ConfigSetList{}
	if err := r.Client.List(ctx, configSetList); err != nil {
		log.Error("cannot list object", "error", err)
		return
	}

	// when config changes and is part of a configset we need to reconcile the configset
	for _, configSet := range configSetList.Items {
		for _, ownerref := range config.OwnerReferences {
			if ownerref.APIVersion == configSet.APIVersion &&
				ownerref.Kind == configSet.Kind &&
				ownerref.Name == configSet.Name &&
				ownerref.UID == configSet.UID {

				key := types.NamespacedName{
					Name:      configSet.GetName(),
					Namespace: configSet.GetNamespace(),
				}
				log.Info("event requeue", "key", key.String())
				queue.Add(reconcile.Request{NamespacedName: key})
				return // these should be 1 configset for a config
			}
		}
	}
}
