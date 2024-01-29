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
	"strings"
	"sync"

	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newTargets(n int) *targets {
	return &targets{
		targets: make(map[string]*invv1alpha1.Target, n),
	}
}

type targets struct {
	m       sync.RWMutex
	targets map[string]*invv1alpha1.Target
}

func (r *targets) add(key string, t *invv1alpha1.Target) {
	r.m.Lock()
	defer r.m.Unlock()
	r.targets[key] = t
}

func (r *targets) del(key string) {
	r.m.Lock()
	defer r.m.Unlock()
	delete(r.targets, key)
}

func (r *targets) get(key string) (*invv1alpha1.Target, bool) {
	r.m.RLock()
	defer r.m.RUnlock()
	t, ok := r.targets[key]
	return t, ok
}

func (r *targets) list() []*invv1alpha1.Target {
	r.m.RLock()
	defer r.m.RUnlock()
	tgts := make([]*invv1alpha1.Target, 0, len(r.targets))
	for _, t := range r.targets {
		tgts = append(tgts, t)
	}
	return tgts
}

func (r *dr) getTargets(ctx context.Context) (*targets, error) {
	// list all targets belonging to this discovery rule
	opts := []client.ListOption{
		client.InNamespace(r.cfg.CR.GetNamespace()),
		client.MatchingLabels{invv1alpha1.LabelKeyDiscoveryRule: r.cfg.CR.GetName()},
	}

	targetList := &invv1alpha1.TargetList{}
	if err := r.client.List(ctx, targetList, opts...); err != nil {
		return nil, err
	}
	targets := newTargets(len(targetList.Items))
	for _, target := range targetList.Items {
		target := target
		targets.add(strings.Split(target.Spec.Address, ":")[0], &target)
	}
	return targets, nil
}
