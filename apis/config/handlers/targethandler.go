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

package handlers

import (
	"context"

	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ListConfigsByTarget(ctx context.Context, c client.Client, targetNamespace, targetName, lookupNamespace string) (map[string]*config.Config, error) {
	v1alpha1configList := &configv1alpha1.ConfigList{}
	if err := c.List(ctx, v1alpha1configList,
		client.InNamespace(lookupNamespace),
		client.MatchingLabels{
			config.TargetNamespaceKey: targetNamespace,
			config.TargetNameKey:      targetName,
		},
	); err != nil {
		return nil, err
	}

	configList := &config.ConfigList{}
	if err := configv1alpha1.Convert_v1alpha1_ConfigList_To_config_ConfigList(v1alpha1configList, configList, nil); err != nil {
		return nil, err
	}

	result := make(map[string]*config.Config, len(configList.Items))
	for i := range configList.Items {
		cfg := &configList.Items[i]
		result[cfg.Name] = cfg
	}
	return result, nil
}
