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

package configserver

import (
	"context"

	configv1alpha1 "github.com/iptecharch/config-server/apis/config/v1alpha1"
	"github.com/iptecharch/config-server/pkg/store"
	"github.com/iptecharch/config-server/pkg/store/file"
	"github.com/iptecharch/config-server/pkg/target"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apiserver/pkg/server/storage"
	"k8s.io/apiserver/pkg/storage/storagebackend"
	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
)



func NewConfig(ctx context.Context, client client.Client, scheme *runtime.Scheme, targetStore store.Storer[target.Context]) (*Cfg, error) {
	newConfigFn := func() runtime.Object { return configv1alpha1.BuildConfig(metav1.ObjectMeta{}, configv1alpha1.ConfigSpec{}, configv1alpha1.ConfigStatus{}) }
	newConfigSetFn := func() runtime.Object { return configv1alpha1.BuildConfigSet(metav1.ObjectMeta{}, configv1alpha1.ConfigSetSpec{}, configv1alpha1.ConfigSetStatus{}) }
	newCfgFn := func() resource.Object { return configv1alpha1.BuildConfig(metav1.ObjectMeta{}, configv1alpha1.ConfigSpec{}, configv1alpha1.ConfigStatus{}) }
	newCfgSetFn := func() resource.Object { return configv1alpha1.BuildConfigSet(metav1.ObjectMeta{}, configv1alpha1.ConfigSetSpec{}, configv1alpha1.ConfigSetStatus{}) }

	configStore, err := createStore(ctx, newConfigFn, newCfgFn, scheme, "config")
	if err != nil {
		return nil, err
	}
	configSetStore, err := createStore(ctx, newConfigSetFn, newCfgSetFn, scheme, "configset")
	if err != nil {
		return nil, err
	}
	return &Cfg{
		client:         client,
		configStore:    configStore,
		configSetStore: configSetStore,
		targetStore:    targetStore,
	}, nil
}

type Cfg struct {
	client         client.Client
	configStore    store.Storer[runtime.Object]
	configSetStore store.Storer[runtime.Object]
	targetStore    store.Storer[target.Context]
}

type newObjFn func() runtime.Object
type newResFn func() resource.Object

func createStore(ctx context.Context, newObjFn newObjFn, newResFn newResFn, scheme *runtime.Scheme, rootPath string) (store.Storer[runtime.Object], error) {
	obj := newResFn()
	gr := obj.GetGroupVersionResource().GroupResource()
	codec, _, err := storage.NewStorageCodec(storage.StorageCodecConfig{
		StorageMediaType:  runtime.ContentTypeJSON,
		StorageSerializer: serializer.NewCodecFactory(scheme),
		StorageVersion:    scheme.PrioritizedVersionsForGroup(obj.GetGroupVersionResource().Group)[0],
		MemoryVersion:     scheme.PrioritizedVersionsForGroup(obj.GetGroupVersionResource().Group)[0],
		Config:            storagebackend.Config{}, // useless fields..
	})

	if err != nil {
		return nil, err
	}
	return file.NewStore[runtime.Object](&file.Config[runtime.Object]{
		GroupResource: gr,
		RootPath:      rootPath,
		Codec:         codec,
		NewFunc:       newObjFn,
	})
}
