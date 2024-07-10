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

package config

import (
	"context"
	"fmt"

	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	"github.com/henderiw/apiserver-store/pkg/generic/registry"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
)

const (
	RunningConfigPlural   = "runningconfigs"
	RunningConfigSingular = "runningconfig"
)

var (
	RunningConfigShortNames = []string{}
	RunningConfigCategories = []string{"sdc", "knet"}
)

// +k8s:deepcopy-gen=false
var _ resource.Object = &RunningConfig{}
var _ resource.ObjectList = &RunningConfigList{}

func (RunningConfig) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    SchemeGroupVersion.Group,
		Version:  SchemeGroupVersion.Version,
		Resource: RunningConfigPlural,
	}
}

// IsStorageVersion returns true -- RunningConfig is used as the internal version.
// IsStorageVersion implements resource.Object
func (RunningConfig) IsStorageVersion() bool {
	return true
}

// NamespaceScoped returns if RunningConfig is a namespaced resource.
// NamespaceScoped implements resource.Object
func (RunningConfig) NamespaceScoped() bool {
	return true
}

// GetObjectMeta implements resource.Object
// GetObjectMeta implements resource.Object
func (r *RunningConfig) GetObjectMeta() *metav1.ObjectMeta {
	return &r.ObjectMeta
}

// GetSingularName returns the singular name of the resource
// GetSingularName implements resource.Object
func (RunningConfig) GetSingularName() string {
	return RunningConfigSingular
}

// GetShortNames returns the shortnames for the resource
// GetShortNames implements resource.Object
func (RunningConfig) GetShortNames() []string {
	return RunningConfigShortNames
}

// GetCategories return the categories of the resource
// GetCategories implements resource.Object
func (RunningConfig) GetCategories() []string {
	return RunningConfigCategories
}

// New return an empty resource
// New implements resource.Object
func (RunningConfig) New() runtime.Object {
	return &RunningConfig{}
}

// NewList return an empty resourceList
// NewList implements resource.Object
func (RunningConfig) NewList() runtime.Object {
	return &RunningConfigList{}
}

// GetListMeta returns the ListMeta
// GetListMeta implements the resource.ObjectList
func (r *RunningConfigList) GetListMeta() *metav1.ListMeta {
	return &r.ListMeta
}

// TableConvertor return the table format of the resource
func (r *RunningConfig) TableConvertor() func(gr schema.GroupResource) rest.TableConvertor {
	return func(gr schema.GroupResource) rest.TableConvertor {
		return registry.NewTableConverter(
			gr,
			func(obj runtime.Object) []interface{} {
				RunningConfig, ok := obj.(*RunningConfig)
				if !ok {
					return nil
				}
				return []interface{}{
					RunningConfig.Name,
				}
			},
			[]metav1.TableColumnDefinition{
				{Name: "Name", Type: "string"},
			},
		)
	}
}

// FieldLabelConversion is the schema conversion function for normalizing the FieldSelector for the resource
func (r *RunningConfig) FieldLabelConversion() runtime.FieldLabelConversionFunc {
	return func(label, value string) (internalLabel, internalValue string, err error) {
		switch label {
		case "metadata.name":
			return label, value, nil
		case "metadata.namespace":
			return label, value, nil
		default:
			return "", "", fmt.Errorf("%q is not a known field selector", label)
		}
	}
}

func (r *RunningConfig) FieldSelector() func(ctx context.Context, fieldSelector fields.Selector) (resource.Filter, error) {
	return func(ctx context.Context, fieldSelector fields.Selector) (resource.Filter, error) {
		var filter *RunningConfigFilter

		// add the namespace to the list
		namespace, ok := genericapirequest.NamespaceFrom(ctx)
		if fieldSelector == nil {
			if ok {
				return &ConfigFilter{Namespace: namespace}, nil
			}
			return filter, nil
		}
		requirements := fieldSelector.Requirements()
		for _, requirement := range requirements {
			filter = &RunningConfigFilter{}
			switch requirement.Operator {
			case selection.Equals, selection.DoesNotExist:
				if requirement.Value == "" {
					return filter, apierrors.NewBadRequest(fmt.Sprintf("unsupported fieldSelector value %q for field %q with operator %q", requirement.Value, requirement.Field, requirement.Operator))
				}
			default:
				return filter, apierrors.NewBadRequest(fmt.Sprintf("unsupported fieldSelector operator %q for field %q", requirement.Operator, requirement.Field))
			}

			switch requirement.Field {
			case "metadata.name":
				filter.Name = requirement.Value
			case "metadata.namespace":
				filter.Namespace = requirement.Value
			default:
				return filter, apierrors.NewBadRequest(fmt.Sprintf("unknown fieldSelector field %q", requirement.Field))
			}
		}
		// add namespace to the filter selector if specified
		if ok {
			if filter != nil {
				filter.Namespace = namespace
			} else {
				filter = &RunningConfigFilter{Namespace: namespace}
			}
			return filter, nil
		}

		return &ConfigFilter{}, nil
	}

}

type RunningConfigFilter struct {
	// Name filters by the name of the objects
	Name string `protobuf:"bytes,1,opt,name=name"`

	// Namespace filters by the namespace of the objects
	Namespace string `protobuf:"bytes,2,opt,name=namespace"`
}

func (r *RunningConfigFilter) Filter(ctx context.Context, obj runtime.Object) bool {
	f := false // result of the previous filter
	o, ok := obj.(*Config)
	if !ok {
		return f
	}
	if r.Name != "" {
		if o.GetName() == r.Name {
			f = false
		} else {
			f = true
		}
	}
	if r.Namespace != "" {
		if o.GetNamespace() == r.Namespace {
			f = false
		} else {
			f = true
		}
	}
	return f
}
