/*
Copyright 2025 Nokia.

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

package rollout

import (
	"context"
	"sort"
	"testing"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	memstore "github.com/henderiw/apiserver-store/pkg/storebackend/memory"
	configapi "github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestGetToBeDeletedConfigsNew(t *testing.T) {
	cases := map[string]struct {
		existingConfigs   map[string][]string
		newConfigs        map[string][]string
		expectedDeletions map[string][]string
	}{
		"Update": {
			existingConfigs: map[string][]string{
				"target1": {"config1"},
				"target2": {"config2"},
			},
			newConfigs: map[string][]string{
				"target2": {"config2"},
			},
			expectedDeletions: map[string][]string{
				"target1": {"config1"},
			},
		},
		"UpdateMix": {
			existingConfigs: map[string][]string{
				"target1": {"config10", "config11", "config12", "config13", "config14"},
				"target2": {"config20", "config21", "config22", "config23", "config24"},
			},
			newConfigs: map[string][]string{
				"target1": {"config10", "config15"},
				"target2": {"config28", "config23", "config20"},
			},
			expectedDeletions: map[string][]string{
				"target1": {"config11", "config12", "config13", "config14"},
				"target2": {"config21", "config22", "config24"},
			},
		},
		"Delete": {
			existingConfigs: map[string][]string{
				"target1": {"config1"},
				"target2": {"config2"},
			},
			newConfigs: map[string][]string{},
			expectedDeletions: map[string][]string{
				"target1": {"config1"},
				"target2": {"config2"},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			existingConfigs := getExistingConfigs(tc.existingConfigs)
			newTargetUpdateConfigStore := getnewConfigStore(tc.newConfigs)

			deleteStore, err := getToBeDeletedconfigs(ctx, newTargetUpdateConfigStore, existingConfigs)
			assert.NoError(t, err, "Function should not return an error")

			actualDeletions := make(map[string][]string)
			deleteStore.List(ctx, func(ctx context.Context, targetKey storebackend.Key, configStore storebackend.Storer[*configapi.Config]) {
				configNames := []string{}
				configStore.List(ctx, func(ctx context.Context, configKey storebackend.Key, config *configapi.Config) {
					configNames = append(configNames, config.Name)
				})
				sort.Strings(configNames)
				actualDeletions[targetKey.Name] = configNames
			})
			// 🔍 Ensure the deletions match expectations
			assert.Equal(t, tc.expectedDeletions, actualDeletions, "Deleted configs should match expected deletions")
		})
	}
}

func getExistingConfigs(existingConfigs map[string][]string) *configv1alpha1.ConfigList {
	existingConfigList := &configv1alpha1.ConfigList{
		Items: []configv1alpha1.Config{},
	}
	for target, configNames := range existingConfigs {
		for _, configName := range configNames {
			config := configv1alpha1.BuildConfig(metav1.ObjectMeta{
				Name: configName, Namespace: namespace,
				Labels: map[string]string{
					configapi.TargetNameKey:      target,
					configapi.TargetNamespaceKey: namespace,
				},
			}, configv1alpha1.ConfigSpec{}, configv1alpha1.ConfigStatus{})
			existingConfigList.Items = append(existingConfigList.Items, *config)
		}

	}
	return existingConfigList
}

func getnewConfigStore(newConfigs map[string][]string) storebackend.Storer[storebackend.Storer[*configapi.Config]] {
	ctx := context.Background()
	newTargetConfigStore := memstore.NewStore[storebackend.Storer[*configapi.Config]]()
	for target, configNames := range newConfigs {
		for _, configName := range configNames {
			targetKey := storebackend.KeyFromNSN(types.NamespacedName{Name: target, Namespace: namespace})
			target1ConfigStore, err := newTargetConfigStore.Get(ctx, targetKey)
			if err != nil {
				target1ConfigStore = memstore.NewStore[*configapi.Config]()
				_ = newTargetConfigStore.Create(ctx, targetKey, target1ConfigStore)
			}

			config := configapi.BuildConfig(metav1.ObjectMeta{
				Name: configName, Namespace: namespace,
				Labels: map[string]string{
					configapi.TargetNameKey:      target,
					configapi.TargetNamespaceKey: namespace,
				},
			}, configapi.ConfigSpec{})
			configKey := storebackend.KeyFromNSN(types.NamespacedName{Name: configName, Namespace: namespace})
			_ = target1ConfigStore.Create(ctx, configKey, config)

			newTargetConfigStore.Update(ctx, targetKey, target1ConfigStore)
		}

	}
	return newTargetConfigStore
}
