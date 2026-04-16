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
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type TargetForConfigEventHandler struct {
	Client         client.Client
	ControllerName string
}

// Create enqueues a request
func (r *TargetForConfigEventHandler) Create(ctx context.Context, evt event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.Object, q)
}

// Create enqueues a request
func (r *TargetForConfigEventHandler) Update(ctx context.Context, evt event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.ObjectOld, q)
	r.add(ctx, evt.ObjectNew, q)
}

// Create enqueues a request
func (r *TargetForConfigEventHandler) Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.Object, q)
}

// Create enqueues a request
func (r *TargetForConfigEventHandler) Generic(ctx context.Context, evt event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.Object, q)
}

func (r *TargetForConfigEventHandler) add(ctx context.Context, obj runtime.Object, queue adder) {
	target, ok := obj.(*configv1alpha1.Target)
	if !ok {
		return
	}
	ctx = ctrlconfig.InitContext(ctx, r.ControllerName, types.NamespacedName{Namespace: "target-event", Name: target.GetName()})
	log := log.FromContext(ctx)

	log.Debug("event", "gvk", configv1alpha1.TargetGroupVersionKind.String(), "name", target.GetName())

	// list all the configs of the particular target that got changed
	opts := []client.ListOption{
		client.InNamespace(target.Namespace),
		client.MatchingLabels{
			config.TargetNameKey:      target.GetName(),
			config.TargetNamespaceKey: target.GetNamespace(),
		},
	}
	configs := &configv1alpha1.ConfigList{}
	if err := r.Client.List(ctx, configs, opts...); err != nil {
		log.Error("cannot list configs", "error", err)
		return
	}
	for _, config := range configs.Items {
		key := types.NamespacedName{
			Namespace: config.Namespace,
			Name:      config.Name}
		log.Debug("event requeue config", "key", key.String())
		queue.Add(reconcile.Request{NamespacedName: key})
	}
}
