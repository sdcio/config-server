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

package discoveryeventhandler

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

type TargetConnProfileEventHandler struct {
	Client  client.Client
	ObjList invv1alpha1.DiscoveryObjectList
}

// Create enqueues a request for all ip allocation within the ipam
func (r *TargetConnProfileEventHandler) Create(evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	r.add(evt.Object, q)
}

// Create enqueues a request for all ip allocation within the ipam
func (r *TargetConnProfileEventHandler) Update(evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	r.add(evt.ObjectOld, q)
	r.add(evt.ObjectNew, q)
}

// Create enqueues a request for all ip allocation within the ipam
func (r *TargetConnProfileEventHandler) Delete(evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
	r.add(evt.Object, q)
}

// Create enqueues a request for all ip allocation within the ipam
func (r *TargetConnProfileEventHandler) Generic(evt event.GenericEvent, q workqueue.RateLimitingInterface) {
	r.add(evt.Object, q)
}

func (r *TargetConnProfileEventHandler) add(obj runtime.Object, queue adder) {
	cr, ok := obj.(*invv1alpha1.TargetConnectionProfile)
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
	drs := r.ObjList
	if err := r.Client.List(ctx, drs, opts...); err != nil {
		log.Error(err, "cannot list discovery rules")
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
		if dr.GetDiscoveryParameters().ConnectivityProfile.ConnectionProfile == cr.GetName() {
			key := types.NamespacedName{
				Namespace: dr.GetNamespace(),
				Name:      dr.GetName()}
			log.Info("event requeue target", "key", key.String())
			queue.Add(reconcile.Request{NamespacedName: key})
			continue
		}
	}
}
