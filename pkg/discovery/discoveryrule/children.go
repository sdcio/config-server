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

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *dr) deleteUnWantedChildren(ctx context.Context) {
	log := log.FromContext(ctx)
	if err := r.deleteUnWantedTargets(ctx); err != nil {
		log.Error("cannot delete unwanted target children", "err", err.Error())
	}
	if err := r.deleteUnwantedDeviations(ctx); err != nil {
		log.Error("cannot delete unwanted unmanagedConfig children", "err", err.Error())
	}
}

func (r *dr) deleteUnWantedTargets(ctx context.Context) error {
	log := log.FromContext(ctx)
	// list all targets belonging to this discovery rule
	opts := []client.ListOption{
		client.InNamespace(r.cfg.CR.GetNamespace()),
		client.MatchingLabels{invv1alpha1.LabelKeyDiscoveryRule: r.cfg.CR.GetName()},
	}

	targetList := &invv1alpha1.TargetList{}
	if err := r.client.List(ctx, targetList, opts...); err != nil {
		return err
	}

	for _, target := range targetList.Items {
		target := target
		found := false
		keys := []string{}
		if err := r.children.List(ctx, func(ctx context.Context, key storebackend.Key, data string) {
			keys = append(keys, key.Name)
			if key.Name == target.Name {
				found = true
			}
		}); err != nil {
			log.Error("liust failed", "err", err)
			return err
		}
		if !found {
			log.Info("target delete, not available as child", "name", target.Name, "children", keys)
			if err := r.client.Delete(ctx, &target); err != nil {
				log.Error("cannot delete target")
			}
		}
	}
	return nil
}

func (r *dr) deleteUnwantedDeviations(ctx context.Context) error {
	log := log.FromContext(ctx)
	// list all targets belonging to this discovery rule
	opts := []client.ListOption{
		client.InNamespace(r.cfg.CR.GetNamespace()),
		client.MatchingLabels{invv1alpha1.LabelKeyDiscoveryRule: r.cfg.CR.GetName()},
	}

	deviationList := &configv1alpha1.DeviationList{}
	if err := r.client.List(ctx, deviationList, opts...); err != nil {
		return err
	}

	for _, deviation := range deviationList.Items {
		found := false
		if err := r.children.List(ctx, func(ctx context.Context, key storebackend.Key, data string) {
			if key.Name == deviation.Name {
				found = true
			}
		}); err != nil {
			log.Error("liust failed", "err", err)
			return err
		}
		if !found {
			if err := r.client.Delete(ctx, &deviation); err != nil {
				log.Error("cannot delete deviation", "err", err)
			}
		}
	}
	return nil
}
