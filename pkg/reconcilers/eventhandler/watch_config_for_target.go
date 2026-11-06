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

// ConfigForTargetEventHandler maps a Config event onto a request for the
// Target named in the Config's target labels. The request is enqueued even
// when that Target does not exist, so the reconciler can report it on the
// Config (sdcio/config-server#469).
type ConfigForTargetEventHandler struct {
	Client         client.Client
	ControllerName string
}

// Create enqueues a request
func (r *ConfigForTargetEventHandler) Create(ctx context.Context, evt event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.Object, q)
}

// Update enqueues a request for the old and the new target (the target
// labels may have changed)
func (r *ConfigForTargetEventHandler) Update(ctx context.Context, evt event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.ObjectOld, q)
	r.add(ctx, evt.ObjectNew, q)
}

// Delete enqueues a request
func (r *ConfigForTargetEventHandler) Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.Object, q)
}

// Generic enqueues a request
func (r *ConfigForTargetEventHandler) Generic(ctx context.Context, evt event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.Object, q)
}

func (r *ConfigForTargetEventHandler) add(ctx context.Context, obj runtime.Object, queue adder) {
	config, ok := obj.(*configv1alpha1.Config)
	if !ok {
		return
	}
	ctx = ctrlconfig.InitContext(ctx, r.ControllerName, types.NamespacedName{Namespace: "config-event", Name: config.GetName()})
	log := log.FromContext(ctx)

	targetKey, err := config.GetTargetNamespaceName()
	if err != nil || targetKey == nil {
		// No (valid) target labels: there is no Target request to enqueue.
		// Such Configs should be rejected by Config validation instead.
		log.Error("config has no valid target reference, not enqueued",
			"config", types.NamespacedName{Namespace: config.GetNamespace(), Name: config.GetName()}.String(),
			"error", err)
		return
	}
	queue.Add(reconcile.Request{NamespacedName: *targetKey})
}
