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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type TargetConnProfileForDiscoveryRuleEventHandler struct {
	Client  client.Client
}

// Create enqueues a request
func (r *TargetConnProfileForDiscoveryRuleEventHandler) Create(ctx context.Context, evt event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.Object, q)
}

// Create enqueues a request
func (r *TargetConnProfileForDiscoveryRuleEventHandler) Update(ctx context.Context, evt event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.ObjectOld, q)
	r.add(ctx, evt.ObjectNew, q)
}

// Create enqueues a request
func (r *TargetConnProfileForDiscoveryRuleEventHandler) Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.Object, q)
}

// Create enqueues a request
func (r *TargetConnProfileForDiscoveryRuleEventHandler) Generic(ctx context.Context, evt event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.Object, q)
}

func (r *TargetConnProfileForDiscoveryRuleEventHandler) add(ctx context.Context, obj runtime.Object, queue adder) {
	cr, ok := obj.(*invv1alpha1.TargetConnectionProfile)
	if !ok {
		return
	}

	log := log.FromContext(ctx)
	log.Info("event", "gvk", invv1alpha1.TargetConnectionProfileGroupVersionKind.String(), "name", cr.GetName())

	// if the endpoint was not claimed, reconcile links whose condition is
	// not true -> this allows the links to reevaluate the endpoints
	opts := []client.ListOption{
		client.InNamespace(cr.Namespace),
	}
	drs :=  &invv1alpha1.DiscoveryRuleList{}
	if err := r.Client.List(ctx, drs, opts...); err != nil {
		log.Error("cannot list discovery rules", "error", err)
		return
	}
	for _, dr := range drs.GetItems() {
		// check if the connection profile is referenced in the discoveryProfile
		if dr.GetDiscoveryParameters().DiscoveryProfile != nil {
			for _, connProfile := range dr.GetDiscoveryParameters().DiscoveryProfile.ConnectionProfiles {
				if connProfile == cr.GetName() {
					key := types.NamespacedName{
						Namespace: dr.GetNamespace(),
						Name:      dr.GetName()}
					log.Info("event requeue target", "key", key.String())
					queue.Add(reconcile.Request{NamespacedName: key})
					continue
				}
			}
		}

		// check if the connection profile is referenced in the ConnectivityProfile
		if *dr.GetDiscoveryParameters().TargetConnectionProfiles[0].SyncProfile == cr.GetName() {
			key := types.NamespacedName{
				Namespace: dr.GetNamespace(),
				Name:      dr.GetName()}
			log.Info("event requeue target", "key", key.String())
			queue.Add(reconcile.Request{NamespacedName: key})
			continue
		}
	}
}
