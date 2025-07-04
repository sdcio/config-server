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
// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	v1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	configv1alpha1 "github.com/sdcio/config-server/pkg/generated/clientset/versioned/typed/config/v1alpha1"
	gentype "k8s.io/client-go/gentype"
)

// fakeRunningConfigs implements RunningConfigInterface
type fakeRunningConfigs struct {
	*gentype.FakeClientWithList[*v1alpha1.RunningConfig, *v1alpha1.RunningConfigList]
	Fake *FakeConfigV1alpha1
}

func newFakeRunningConfigs(fake *FakeConfigV1alpha1, namespace string) configv1alpha1.RunningConfigInterface {
	return &fakeRunningConfigs{
		gentype.NewFakeClientWithList[*v1alpha1.RunningConfig, *v1alpha1.RunningConfigList](
			fake.Fake,
			namespace,
			v1alpha1.SchemeGroupVersion.WithResource("runningconfigs"),
			v1alpha1.SchemeGroupVersion.WithKind("RunningConfig"),
			func() *v1alpha1.RunningConfig { return &v1alpha1.RunningConfig{} },
			func() *v1alpha1.RunningConfigList { return &v1alpha1.RunningConfigList{} },
			func(dst, src *v1alpha1.RunningConfigList) { dst.ListMeta = src.ListMeta },
			func(list *v1alpha1.RunningConfigList) []*v1alpha1.RunningConfig {
				return gentype.ToPointerSlice(list.Items)
			},
			func(list *v1alpha1.RunningConfigList, items []*v1alpha1.RunningConfig) {
				list.Items = gentype.FromPointerSlice(items)
			},
		),
		fake,
	}
}
