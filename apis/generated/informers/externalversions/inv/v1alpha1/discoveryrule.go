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
	"context"
	time "time"

	versioned "github.com/iptecharch/config-server/apis/generated/clientset/versioned"
	internalinterfaces "github.com/iptecharch/config-server/apis/generated/informers/externalversions/internalinterfaces"
	v1alpha1 "github.com/iptecharch/config-server/apis/generated/listers/inv/v1alpha1"
	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// DiscoveryRuleInformer provides access to a shared informer and lister for
// DiscoveryRules.
type DiscoveryRuleInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha1.DiscoveryRuleLister
}

type discoveryRuleInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewDiscoveryRuleInformer constructs a new informer for DiscoveryRule type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewDiscoveryRuleInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredDiscoveryRuleInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredDiscoveryRuleInformer constructs a new informer for DiscoveryRule type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredDiscoveryRuleInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.InvV1alpha1().DiscoveryRules(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.InvV1alpha1().DiscoveryRules(namespace).Watch(context.TODO(), options)
			},
		},
		&invv1alpha1.DiscoveryRule{},
		resyncPeriod,
		indexers,
	)
}

func (f *discoveryRuleInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredDiscoveryRuleInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *discoveryRuleInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&invv1alpha1.DiscoveryRule{}, f.defaultInformer)
}

func (f *discoveryRuleInformer) Lister() v1alpha1.DiscoveryRuleLister {
	return v1alpha1.NewDiscoveryRuleLister(f.Informer().GetIndexer())
}
