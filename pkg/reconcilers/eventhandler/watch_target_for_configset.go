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
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type TargetForConfigSet struct {
	Client         client.Client
	ControllerName string
}

// Create enqueues a request
func (r *TargetForConfigSet) Create(ctx context.Context, evt event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.Object, q)
}

// Create enqueues a request
func (r *TargetForConfigSet) Update(ctx context.Context, evt event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.ObjectOld, q)
	r.add(ctx, evt.ObjectNew, q)
}

// Create enqueues a request
func (r *TargetForConfigSet) Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.Object, q)
}

// Create enqueues a request
func (r *TargetForConfigSet) Generic(ctx context.Context, evt event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.Object, q)
}

func (r *TargetForConfigSet) add(ctx context.Context, obj runtime.Object, queue adder) {
	target, ok := obj.(*invv1alpha1.Target)
	if !ok {
		return
	}
	ctx = ctrlconfig.InitContext(ctx, r.ControllerName, types.NamespacedName{Namespace: "target-event", Name: target.GetName()})
	log := log.FromContext(ctx)

	log.Debug("event", "gvk", invv1alpha1.TargetGroupVersionKind.String(), "name", target.GetName())

	// list the configsets and see
	opts := []client.ListOption{
		client.InNamespace(target.Namespace),
	}
	configsets := &configv1alpha1.ConfigSetList{}
	if err := r.Client.List(ctx, configsets, opts...); err != nil {
		log.Error("cannot list configsets", "error", err)
		return
	}

	for _, configset := range configsets.Items {
		selector, err := metav1.LabelSelectorAsSelector(configset.Spec.Target.TargetSelector)
		if err != nil {
			log.Error("cannot get label selector from configset", "name", configset.Name, "error", err.Error())
			continue
		}
		found := false
		if selector.Matches(labels.Set(target.GetLabels())) {
			log.Debug("event target selector matches")
			// we always requeue since it allows to handle delete of targets that were previously there
			key := types.NamespacedName{
				Namespace: configset.Namespace,
				Name:      configset.Name}
			log.Debug("event requeue configset with target create", "key", key.String(), "target", target.GetName())
			queue.Add(reconcile.Request{NamespacedName: key})
		} else {
			// check if the target was part of the target list before, if so requeue it
			for _, configSetTarget := range configset.Status.Targets {
				if configSetTarget.Name == target.Name {
					found = true
					break
				}
			}
			if found {
				key := types.NamespacedName{
					Namespace: configset.Namespace,
					Name:      configset.Name}
				log.Debug("event requeue configset with target delete", "key", key.String(), "target", target.GetName())
				queue.Add(reconcile.Request{NamespacedName: key})
			}
		}
	}
}
