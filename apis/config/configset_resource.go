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
	"github.com/sdcio/config-server/apis/condition"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/validation/field"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
)

const (
	ConfigSetPlural   = "configs"
	ConfigSetSingular = "config"
)

var (
	ConfigSetShortNames = []string{}
	ConfigSetCategories = []string{"sdc", "knet"}
)

// +k8s:deepcopy-gen=false
var _ resource.Object = &ConfigSet{}
var _ resource.ObjectList = &ConfigSetList{}

func (ConfigSet) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    SchemeGroupVersion.Group,
		Version:  SchemeGroupVersion.Version,
		Resource: ConfigPlural,
	}
}

// IsStorageVersion returns true -- ConfigSet is used as the internal version.
// IsStorageVersion implements resource.Object
func (ConfigSet) IsStorageVersion() bool {
	return true
}

// NamespaceScoped returns if ConfigSet is a namespaced resource.
// NamespaceScoped implements resource.Object
func (ConfigSet) NamespaceScoped() bool {
	return true
}

// GetObjectMeta implements resource.Object
func (r *ConfigSet) GetObjectMeta() *metav1.ObjectMeta {
	return &r.ObjectMeta
}

// GetSingularName returns the singular name of the resource
// GetSingularName implements resource.Object
func (ConfigSet) GetSingularName() string {
	return ConfigSingular
}

// GetShortNames returns the shortnames for the resource
// GetShortNames implements resource.Object
func (ConfigSet) GetShortNames() []string {
	return ConfigShortNames
}

// GetCategories return the categories of the resource
// GetCategories implements resource.Object
func (ConfigSet) GetCategories() []string {
	return ConfigCategories
}

// New return an empty resource
// New implements resource.Object
func (ConfigSet) New() runtime.Object {
	return &Config{}
}

// NewList return an empty resourceList
// NewList implements resource.Object
func (ConfigSet) NewList() runtime.Object {
	return &ConfigList{}
}

// GetStatus return the resource.StatusSubResource interface
func (r *ConfigSet) GetStatus() resource.StatusSubResource {
	return r.Status
}

// SubResourceName resturns the name of the subresource
// SubResourceName implements the resource.StatusSubResource
func (ConfigSetStatus) SubResourceName() string {
	return fmt.Sprintf("%s/%s", ConfigPlural, "status")
}

// CopyTo copies the content of the status subresource to a parent resource.
// CopyTo implements the resource.StatusSubResource
func (r ConfigSetStatus) CopyTo(obj resource.ObjectWithStatusSubResource) {
	cfg, ok := obj.(*ConfigSet)
	if ok {
		cfg.Status = r
	}
}

// GetListMeta returns the ListMeta
// GetListMeta implements the resource.ObjectList
func (r *ConfigSetList) GetListMeta() *metav1.ListMeta {
	return &r.ListMeta
}

// TableConvertor return the table format of the resource
func (r *ConfigSet) TableConvertor() func(gr schema.GroupResource) rest.TableConvertor {
	return func(gr schema.GroupResource) rest.TableConvertor {
		return registry.NewTableConverter(
			gr,
			func(obj runtime.Object) []interface{} {
				configset, ok := obj.(*ConfigSet)
				if !ok {
					return nil
				}
				return []interface{}{
					configset.Name,
					configset.GetCondition(condition.ConditionTypeReady).Status,
					len(configset.Status.Targets),
				}
			},
			[]metav1.TableColumnDefinition{
				{Name: "Name", Type: "string"},
				{Name: "Ready", Type: "string"},
				{Name: "Targets", Type: "integer"},
			},
		)
	}
}

// FieldLabelConversion is the schema conversion function for normalizing the FieldSelector for the resource
func (r *ConfigSet) FieldLabelConversion() runtime.FieldLabelConversionFunc {
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

func (r *ConfigSet) FieldSelector() func(ctx context.Context, fieldSelector fields.Selector) (resource.Filter, error) {
	return func(ctx context.Context, fieldSelector fields.Selector) (resource.Filter, error) {
		var filter *ConfigSetFilter

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
			filter = &ConfigSetFilter{}
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
				filter = &ConfigSetFilter{Namespace: namespace}
			}
			return filter, nil
		}

		return &ConfigFilter{}, nil
	}

}

type ConfigSetFilter struct {
	// Name filters by the name of the objects
	Name string `protobuf:"bytes,1,opt,name=name"`

	// Namespace filters by the namespace of the objects
	Namespace string `protobuf:"bytes,2,opt,name=namespace"`
}

func (r *ConfigSetFilter) Filter(ctx context.Context, obj runtime.Object) bool {
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

// ValidateCreate statically validates
func (r *ConfigSet) ValidateCreate(ctx context.Context) field.ErrorList {
	var allErrs field.ErrorList

	if _, err := metav1.LabelSelectorAsSelector(r.Spec.Target.TargetSelector); err != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec.target.selector"),
			r,
			err.Error(),
		))
	}

	return allErrs
}

func (r *ConfigSet) ValidateUpdate(ctx context.Context, old runtime.Object) field.ErrorList {
	var allErrs field.ErrorList

	if _, err := metav1.LabelSelectorAsSelector(r.Spec.Target.TargetSelector); err != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec.target.selector"),
			r,
			err.Error(),
		))
	}

	return allErrs
}
