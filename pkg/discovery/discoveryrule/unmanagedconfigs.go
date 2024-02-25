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

/*

import (
	"context"
	"sync"

	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newChildren(n int) *children {
	return &children{
		targets:          make(map[string]*invv1alpha1.Target, n),
		unmanagedConfigs: make(map[string]*configv1alpha1.UnManagedConfig, n),
	}
}

type children struct {
	m                sync.RWMutex
	targets          map[string]*invv1alpha1.Target
	unmanagedConfigs map[string]*configv1alpha1.UnManagedConfig
}

func (r *children) add(key string, t *configv1alpha1.UnManagedConfig) {
	r.m.Lock()
	defer r.m.Unlock()
	r.unmanagedConfigs[key] = t
}

func (r *unmanagedConfigs) del(key string) {
	r.m.Lock()
	defer r.m.Unlock()
	delete(r.unmanagedConfigs, key)
}

func (r *unmanagedConfigs) list() []*configv1alpha1.UnManagedConfig {
	r.m.RLock()
	defer r.m.RUnlock()
	unmanagedConfigs := make([]*configv1alpha1.UnManagedConfig, 0, len(r.unmanagedConfigs))
	for _, t := range r.unmanagedConfigs {
		unmanagedConfigs = append(unmanagedConfigs, t)
	}
	return unmanagedConfigs
}

func (r *dr) getUnManagedConfigs(ctx context.Context) (*unmanagedConfigs, error) {
	// list all unmanaged configs belonging to this discovery rule
	opts := []client.ListOption{
		client.InNamespace(r.cfg.CR.GetNamespace()),
		client.MatchingLabels{invv1alpha1.LabelKeyDiscoveryRule: r.cfg.CR.GetName()},
	}

	unmanagedConfigList := &configv1alpha1.UnManagedConfigList{}
	if err := r.client.List(ctx, unmanagedConfigList, opts...); err != nil {
		return nil, err
	}
	unmanagedConfigs := newUnManagedConfigs(len(unmanagedConfigList.Items))
	for _, unmanagedConfig := range unmanagedConfigList.Items {
		unmanagedConfig := unmanagedConfig
		unmanagedConfigs.add(unmanagedConfig.Name, &unmanagedConfig)
	}
	return unmanagedConfigs, nil
}
*/
