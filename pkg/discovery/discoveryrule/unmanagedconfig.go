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
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

func (r *dr) applyUnManagedConfigCR(ctx context.Context, targetName string) error {
	newUnManagedConfigCR, err := r.newUnManagedConfigCR(ctx, targetName)
	if err != nil {
		return err
	}
	log := log.FromContext(ctx).With("targetName", newUnManagedConfigCR.Name)

	// check if the target already exists
	curUnManagedConfigCR := &configv1alpha1.UnManagedConfig{}
	if err := r.client.Get(ctx, types.NamespacedName{
		Namespace: newUnManagedConfigCR.Namespace,
		Name:      newUnManagedConfigCR.Name,
	}, curUnManagedConfigCR); err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return err
		}
		log.Info("unmanagedConfig does not exist -> create")

		if err := r.client.Create(ctx, newUnManagedConfigCR); err != nil {
			return err
		}
		return nil
	}
	return nil
}

func (r *dr) newUnManagedConfigCR(_ context.Context, targetName string) (*configv1alpha1.UnManagedConfig, error) {
	labels, err := r.cfg.CR.GetDiscoveryParameters().GetTargetLabels(r.cfg.CR.GetName())
	if err != nil {
		return nil, err
	}
	anno, err := r.cfg.CR.GetDiscoveryParameters().GetTargetAnnotations(r.cfg.CR.GetName())
	if err != nil {
		return nil, err
	}

	return &configv1alpha1.UnManagedConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:        targetName,
			Namespace:   r.cfg.CR.GetNamespace(),
			Labels:      labels,
			Annotations: anno,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: schema.GroupVersion{
						Group:   r.cfg.CR.GetObjectKind().GroupVersionKind().Group,
						Version: r.cfg.CR.GetObjectKind().GroupVersionKind().Version,
					}.String(),
					Kind:       r.cfg.CR.GetObjectKind().GroupVersionKind().Kind,
					Name:       r.cfg.CR.GetName(),
					UID:        r.cfg.CR.GetUID(),
					Controller: ptr.To[bool](true),
				}},
		},
		Spec: configv1alpha1.UnManagedConfigSpec{},
		Status: configv1alpha1.UnManagedConfigStatus{
			Deviations: []configv1alpha1.Deviation{},
		},
	}, nil
}
