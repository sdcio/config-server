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

type DeviationForConfigEventHandler struct {
	Client         client.Client
	ControllerName string
}

// Create enqueues a request
func (r *DeviationForConfigEventHandler) Create(ctx context.Context, evt event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.Object, q)
}

// Create enqueues a request
func (r *DeviationForConfigEventHandler) Update(ctx context.Context, evt event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.ObjectOld, q)
	r.add(ctx, evt.ObjectNew, q)
}

// Create enqueues a request
func (r *DeviationForConfigEventHandler) Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.Object, q)
}

// Create enqueues a request
func (r *DeviationForConfigEventHandler) Generic(ctx context.Context, evt event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.Object, q)
}

func (r *DeviationForConfigEventHandler) add(ctx context.Context, obj runtime.Object, queue adder) {
	deviation, ok := obj.(*configv1alpha1.Deviation)
	if !ok {
		return
	}
	ctx = ctrlconfig.InitContext(ctx, r.ControllerName, types.NamespacedName{Namespace: "deviation-event", Name: deviation.GetName()})
	log := log.FromContext(ctx)
	
	key := types.NamespacedName{
		Namespace: deviation.Namespace,
		Name:      deviation.Name}
	log.Info("event requeue config from deviation", "key", key.String())
	queue.Add(reconcile.Request{NamespacedName: key})
	
}
