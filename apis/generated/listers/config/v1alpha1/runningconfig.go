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
	v1alpha1 "github.com/iptecharch/config-server/apis/config/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// RunningConfigLister helps list RunningConfigs.
// All objects returned here must be treated as read-only.
type RunningConfigLister interface {
	// List lists all RunningConfigs in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.RunningConfig, err error)
	// RunningConfigs returns an object that can list and get RunningConfigs.
	RunningConfigs(namespace string) RunningConfigNamespaceLister
	RunningConfigListerExpansion
}

// runningConfigLister implements the RunningConfigLister interface.
type runningConfigLister struct {
	indexer cache.Indexer
}

// NewRunningConfigLister returns a new RunningConfigLister.
func NewRunningConfigLister(indexer cache.Indexer) RunningConfigLister {
	return &runningConfigLister{indexer: indexer}
}

// List lists all RunningConfigs in the indexer.
func (s *runningConfigLister) List(selector labels.Selector) (ret []*v1alpha1.RunningConfig, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.RunningConfig))
	})
	return ret, err
}

// RunningConfigs returns an object that can list and get RunningConfigs.
func (s *runningConfigLister) RunningConfigs(namespace string) RunningConfigNamespaceLister {
	return runningConfigNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// RunningConfigNamespaceLister helps list and get RunningConfigs.
// All objects returned here must be treated as read-only.
type RunningConfigNamespaceLister interface {
	// List lists all RunningConfigs in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.RunningConfig, err error)
	// Get retrieves the RunningConfig from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.RunningConfig, error)
	RunningConfigNamespaceListerExpansion
}

// runningConfigNamespaceLister implements the RunningConfigNamespaceLister
// interface.
type runningConfigNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all RunningConfigs in the indexer for a given namespace.
func (s runningConfigNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.RunningConfig, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.RunningConfig))
	})
	return ret, err
}

// Get retrieves the RunningConfig from the indexer for a given namespace and name.
func (s runningConfigNamespaceLister) Get(name string) (*v1alpha1.RunningConfig, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("runningconfig"), name)
	}
	return obj.(*v1alpha1.RunningConfig), nil
}
