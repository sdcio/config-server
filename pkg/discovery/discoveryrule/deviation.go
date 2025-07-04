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

	"github.com/henderiw/logger/log"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/sdcio/config-server/apis/config"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
)

func (r *dr) applyTargetDeviationCR(ctx context.Context, target *invv1alpha1.Target) error {
	newUnManagedConfigCR, err := r.newDeviationCR(ctx, target)
	if err != nil {
		return err
	}
	log := log.FromContext(ctx).With("targetName", newUnManagedConfigCR.Name)
	// check if the target already exists
	curDeviationCR := &configv1alpha1.Deviation{}
	if err := r.client.Get(ctx, types.NamespacedName{
		Namespace: newUnManagedConfigCR.Namespace,
		Name:      newUnManagedConfigCR.Name,
	}, curDeviationCR); err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return err
		}
		if err := r.client.Create(ctx, newUnManagedConfigCR); err != nil {
			log.Error("cannot create target deviation", "name", newUnManagedConfigCR.Name, "error", err)
			return err
		}
		return nil
	}
	return nil
}

func (r *dr) newDeviationCR(_ context.Context, target *invv1alpha1.Target) (*configv1alpha1.Deviation, error) {
	labels, err := r.cfg.CR.GetDiscoveryParameters().GetTargetLabels(r.cfg.CR.GetName())
	if err != nil {
		return nil, err
	}
	labels[config.TargetNamespaceKey] = target.Namespace
	labels[config.TargetNameKey] = target.Name
	anno, err := r.cfg.CR.GetDiscoveryParameters().GetTargetAnnotations(r.cfg.CR.GetName())
	if err != nil {
		return nil, err
	}

	return &configv1alpha1.Deviation{
		ObjectMeta: metav1.ObjectMeta{
			Name:        target.Name,
			Namespace:   target.Namespace,
			Labels:      labels,
			Annotations: anno,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: schema.GroupVersion{
						Group:   target.GetObjectKind().GroupVersionKind().Group,
						Version: target.GetObjectKind().GroupVersionKind().Version,
					}.String(),
					Kind:       target.GetObjectKind().GroupVersionKind().Kind,
					Name:       target.GetName(),
					UID:        target.GetUID(),
					Controller: ptr.To[bool](true),
				}},
		},
		Spec: configv1alpha1.DeviationSpec{
			Deviations: []configv1alpha1.ConfigDeviation{},
		},
		Status: configv1alpha1.DeviationStatus{},
	}, nil
}
