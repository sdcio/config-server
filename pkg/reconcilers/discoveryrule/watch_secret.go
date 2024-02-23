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

package discoveryrule

import (
	"context"
	"fmt"

	"github.com/henderiw/logger/log"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type secretEventHandler struct {
	client client.Client
}

// Create enqueues a request for all ip allocation within the ipam
func (r *secretEventHandler) Create(ctx context.Context, evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	r.add(ctx, evt.Object, q)
}

// Create enqueues a request for all ip allocation within the ipam
func (r *secretEventHandler) Update(ctx context.Context, evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	r.add(ctx, evt.ObjectOld, q)
	r.add(ctx, evt.ObjectNew, q)
}

// Create enqueues a request for all ip allocation within the ipam
func (r *secretEventHandler) Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
	r.add(ctx, evt.Object, q)
}

// Create enqueues a request for all ip allocation within the ipam
func (r *secretEventHandler) Generic(ctx context.Context, evt event.GenericEvent, q workqueue.RateLimitingInterface) {
	r.add(ctx, evt.Object, q)
}

func (r *secretEventHandler) add(ctx context.Context, obj runtime.Object, queue adder) {
	cr, ok := obj.(*corev1.Secret)
	if !ok {
		return
	}
	//ctx := context.Background()
	log := log.FromContext(ctx)
	log.Info("event", "gvk", fmt.Sprintf("%s.%s", cr.APIVersion, cr.Kind), "name", cr.GetName())

	// if the endpoint was not claimed, reconcile links whose condition is
	// not true -> this allows the links to reevaluate the endpoints
	opts := []client.ListOption{
		client.InNamespace(cr.Namespace),
	}
	drList := &invv1alpha1.DiscoveryRuleList{}
	if err := r.client.List(ctx, drList, opts...); err != nil {
		log.Error("cannot list targets", "error", err)
		return
	}
	for _, dr := range drList.Items {
		// dr.Spec.DiscoveryProfile is there to protect static discovery profiles
		if dr.Spec.DiscoveryProfile != nil && dr.Spec.DiscoveryProfile.Credentials == cr.GetName() {
			key := types.NamespacedName{
				Namespace: dr.Namespace,
				Name:      dr.Name}
			log.Info("event requeue dr", "key", key.String())
			queue.Add(reconcile.Request{NamespacedName: key})
			return
		}
		for _, targetConnProfile := range dr.Spec.TargetConnectionProfiles {
			if targetConnProfile.Credentials == cr.GetName() {
				key := types.NamespacedName{
					Namespace: dr.Namespace,
					Name:      dr.Name}
				log.Info("event requeue dr", "key", key.String())
				queue.Add(reconcile.Request{NamespacedName: key})
				return
			}
		}
	}
}
