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
// Code generated by informer-gen. DO NOT EDIT.

package v1alpha1

import (
	context "context"
	time "time"

	apisconfigv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	versioned "github.com/sdcio/config-server/pkg/generated/clientset/versioned"
	internalinterfaces "github.com/sdcio/config-server/pkg/generated/informers/externalversions/internalinterfaces"
	configv1alpha1 "github.com/sdcio/config-server/pkg/generated/listers/config/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// ConfigBlameInformer provides access to a shared informer and lister for
// ConfigBlames.
type ConfigBlameInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() configv1alpha1.ConfigBlameLister
}

type configBlameInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewConfigBlameInformer constructs a new informer for ConfigBlame type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewConfigBlameInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredConfigBlameInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredConfigBlameInformer constructs a new informer for ConfigBlame type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredConfigBlameInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.ConfigV1alpha1().ConfigBlames(namespace).List(context.Background(), options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.ConfigV1alpha1().ConfigBlames(namespace).Watch(context.Background(), options)
			},
			ListWithContextFunc: func(ctx context.Context, options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.ConfigV1alpha1().ConfigBlames(namespace).List(ctx, options)
			},
			WatchFuncWithContext: func(ctx context.Context, options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.ConfigV1alpha1().ConfigBlames(namespace).Watch(ctx, options)
			},
		},
		&apisconfigv1alpha1.ConfigBlame{},
		resyncPeriod,
		indexers,
	)
}

func (f *configBlameInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredConfigBlameInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *configBlameInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&apisconfigv1alpha1.ConfigBlame{}, f.defaultInformer)
}

func (f *configBlameInformer) Lister() configv1alpha1.ConfigBlameLister {
	return configv1alpha1.NewConfigBlameLister(f.Informer().GetIndexer())
}
