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
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type SecretForSchemaEventHandler struct {
	Client         client.Client
	ControllerName string
}

// Create enqueues a request
func (r *SecretForSchemaEventHandler) Create(ctx context.Context, evt event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.Object, q)
}

// Create enqueues a request
func (r *SecretForSchemaEventHandler) Update(ctx context.Context, evt event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.ObjectOld, q)
	r.add(ctx, evt.ObjectNew, q)
}

// Create enqueues a request
func (r *SecretForSchemaEventHandler) Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.Object, q)
}

// Create enqueues a request
func (r *SecretForSchemaEventHandler) Generic(ctx context.Context, evt event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.Object, q)
}

func (r *SecretForSchemaEventHandler) add(ctx context.Context, obj runtime.Object, queue adder) {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return
	}
	ctx = ctrlconfig.InitContext(ctx, r.ControllerName, types.NamespacedName{Namespace: "secret-event", Name: secret.GetName()})
	log := log.FromContext(ctx)

	schemaList := &invv1alpha1.SchemaList{}
	if err := r.Client.List(ctx, schemaList); err != nil {
		log.Error("cannot list object", "error", err)
		return
	}

	// when config changes and is part of a configset we need to reconcile the configset
	for _, schema := range schemaList.Items {
		for _, repo := range schema.Spec.Repositories {
			if repo.Credentials == secret.Name {
				key := types.NamespacedName{
					Name:      schema.GetName(),
					Namespace: schema.GetNamespace(),
				}
				log.Info("event requeue", "key", key.String())
				queue.Add(reconcile.Request{NamespacedName: key})
				return // these should be 1 configset for a config
			}
		}
	}
}
