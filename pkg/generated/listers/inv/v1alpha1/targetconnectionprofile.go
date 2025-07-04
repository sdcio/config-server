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
// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	labels "k8s.io/apimachinery/pkg/labels"
	listers "k8s.io/client-go/listers"
	cache "k8s.io/client-go/tools/cache"
)

// TargetConnectionProfileLister helps list TargetConnectionProfiles.
// All objects returned here must be treated as read-only.
type TargetConnectionProfileLister interface {
	// List lists all TargetConnectionProfiles in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*invv1alpha1.TargetConnectionProfile, err error)
	// TargetConnectionProfiles returns an object that can list and get TargetConnectionProfiles.
	TargetConnectionProfiles(namespace string) TargetConnectionProfileNamespaceLister
	TargetConnectionProfileListerExpansion
}

// targetConnectionProfileLister implements the TargetConnectionProfileLister interface.
type targetConnectionProfileLister struct {
	listers.ResourceIndexer[*invv1alpha1.TargetConnectionProfile]
}

// NewTargetConnectionProfileLister returns a new TargetConnectionProfileLister.
func NewTargetConnectionProfileLister(indexer cache.Indexer) TargetConnectionProfileLister {
	return &targetConnectionProfileLister{listers.New[*invv1alpha1.TargetConnectionProfile](indexer, invv1alpha1.Resource("targetconnectionprofile"))}
}

// TargetConnectionProfiles returns an object that can list and get TargetConnectionProfiles.
func (s *targetConnectionProfileLister) TargetConnectionProfiles(namespace string) TargetConnectionProfileNamespaceLister {
	return targetConnectionProfileNamespaceLister{listers.NewNamespaced[*invv1alpha1.TargetConnectionProfile](s.ResourceIndexer, namespace)}
}

// TargetConnectionProfileNamespaceLister helps list and get TargetConnectionProfiles.
// All objects returned here must be treated as read-only.
type TargetConnectionProfileNamespaceLister interface {
	// List lists all TargetConnectionProfiles in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*invv1alpha1.TargetConnectionProfile, err error)
	// Get retrieves the TargetConnectionProfile from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*invv1alpha1.TargetConnectionProfile, error)
	TargetConnectionProfileNamespaceListerExpansion
}

// targetConnectionProfileNamespaceLister implements the TargetConnectionProfileNamespaceLister
// interface.
type targetConnectionProfileNamespaceLister struct {
	listers.ResourceIndexer[*invv1alpha1.TargetConnectionProfile]
}
