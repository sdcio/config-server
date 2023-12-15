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

package discoveryrule

import (
	"context"
	"fmt"

	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type drStaticEventHandler struct {
	client client.Client
}

// Create enqueues a request for all ip allocation within the ipam
func (r *drStaticEventHandler) Create(evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	r.add(evt.Object, q)
}

// Create enqueues a request for all ip allocation within the ipam
func (r *drStaticEventHandler) Update(evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	r.add(evt.ObjectOld, q)
	r.add(evt.ObjectNew, q)
}

// Create enqueues a request for all ip allocation within the ipam
func (r *drStaticEventHandler) Delete(evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
	r.add(evt.Object, q)
}

// Create enqueues a request for all ip allocation within the ipam
func (r *drStaticEventHandler) Generic(evt event.GenericEvent, q workqueue.RateLimitingInterface) {
	r.add(evt.Object, q)
}

func (r *drStaticEventHandler) add(obj runtime.Object, queue adder) {
	cr, ok := obj.(*invv1alpha1.DiscoveryRuleStatic)
	if !ok {
		return
	}
	ctx := context.Background()
	log := log.FromContext(ctx)
	log.Info("event", "gvk", fmt.Sprintf("%s.%s", cr.APIVersion, cr.Kind), "name", cr.GetName())

	// if the endpoint was not claimed, reconcile links whose condition is
	// not true -> this allows the links to reevaluate the endpoints
	opts := []client.ListOption{
		client.InNamespace(cr.Namespace),
	}
	drs := &invv1alpha1.DiscoveryRuleList{}
	if err := r.client.List(ctx, drs, opts...); err != nil {
		log.Error(err, "cannot list links")
		return
	}
	for _, dr := range drs.Items {
		if dr.Spec.DiscoveryRuleRef.Name == cr.GetName() &&
			dr.Spec.DiscoveryRuleRef.APIVersion == invv1alpha1.SchemeGroupVersion.String() &&
			dr.Spec.DiscoveryRuleRef.Kind == invv1alpha1.DiscoveryRuleStaticKind {
			key := types.NamespacedName{
				Namespace: dr.Namespace,
				Name:      dr.Name}
			log.Info("event requeue target", "key", key.String())
			queue.Add(reconcile.Request{NamespacedName: key})
		}
	}
}
