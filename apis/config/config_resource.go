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
	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/apis/condition"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
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
	ConfigPlural   = "configs"
	ConfigSingular = "config"
)

var (
	ConfigShortNames = []string{}
	ConfigCategories = []string{"sdc", "knet"}
)

// +k8s:deepcopy-gen=false
var _ resource.Object = &Config{}
var _ resource.ObjectList = &ConfigList{}

func (Config) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    SchemeGroupVersion.Group,
		Version:  SchemeGroupVersion.Version,
		Resource: ConfigPlural,
	}
}

// IsStorageVersion returns true -- Config is used as the internal version.
// IsStorageVersion implements resource.Object
func (Config) IsStorageVersion() bool {
	return true
}

// NamespaceScoped returns true to indicate Fortune is a namespaced resource.
// NamespaceScoped implements resource.Object
func (Config) NamespaceScoped() bool {
	return true
}

// GetObjectMeta implements resource.Object
// GetObjectMeta implements resource.Object
func (r *Config) GetObjectMeta() *metav1.ObjectMeta {
	return &r.ObjectMeta
}

// GetSingularName returns the singular name of the resource
// GetSingularName implements resource.Object
func (Config) GetSingularName() string {
	return ConfigSingular
}

// GetShortNames returns the shortnames for the resource
// GetShortNames implements resource.Object
func (Config) GetShortNames() []string {
	return ConfigShortNames
}

// GetCategories return the categories of the resource
// GetCategories implements resource.Object
func (Config) GetCategories() []string {
	return ConfigCategories
}

// New return an empty resource
// New implements resource.Object
func (Config) New() runtime.Object {
	return &Config{}
}

// NewList return an empty resourceList
// NewList implements resource.Object
func (Config) NewList() runtime.Object {
	return &ConfigList{}
}

// GetStatus return the resource.StatusSubResource interface
func (r *Config) GetStatus() resource.StatusSubResource {
	return r.Status
}

// SubResourceName resturns the name of the subresource
// SubResourceName implements the resource.StatusSubResource
func (ConfigStatus) SubResourceName() string {
	return fmt.Sprintf("%s/%s", ConfigPlural, "status")
}

// CopyTo copies the content of the status subresource to a parent resource.
// CopyTo implements the resource.StatusSubResource
func (r ConfigStatus) CopyTo(obj resource.ObjectWithStatusSubResource) {
	cfg, ok := obj.(*Config)
	if ok {
		cfg.Status = r
	}
}

// GetListMeta returns the ListMeta
// GetListMeta implements the resource.ObjectList
func (r *ConfigList) GetListMeta() *metav1.ListMeta {
	return &r.ListMeta
}

// TableConvertor return the table format of the resource
func (r *Config) TableConvertor() func(gr schema.GroupResource) rest.TableConvertor {
	return func(gr schema.GroupResource) rest.TableConvertor {
		return registry.NewTableConverter(
			gr,
			func(obj runtime.Object) []interface{} {
				config, ok := obj.(*Config)
				if !ok {
					return nil
				}
				return []interface{}{
					config.Name,
					config.GetCondition(condition.ConditionTypeReady).Status,
					config.GetCondition(condition.ConditionTypeReady).Reason,
					config.GetTarget(),
					config.GetLastKnownGoodSchema().FileString(),
				}
			},
			[]metav1.TableColumnDefinition{
				{Name: "Name", Type: "string"},
				{Name: "Ready", Type: "string"},
				{Name: "Reason", Type: "string"},
				{Name: "Target", Type: "string"},
				{Name: "Schema", Type: "string"},
			},
		)
	}
}

// FieldLabelConversion is the schema conversion function for normalizing the FieldSelector for the resource
func (r *Config) FieldLabelConversion() runtime.FieldLabelConversionFunc {
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

func (r *Config) FieldSelector() func(ctx context.Context, fieldSelector fields.Selector) (resource.Filter, error) {
	return func(ctx context.Context, fieldSelector fields.Selector) (resource.Filter, error) {
		var filter *ConfigFilter

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
			filter = &ConfigFilter{}
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
				filter = &ConfigFilter{Namespace: namespace}
			}
			return filter, nil
		}

		return &ConfigFilter{}, nil
	}

}

type ConfigFilter struct {
	// Name filters by the name of the objects
	Name string `protobuf:"bytes,1,opt,name=name"`

	// Namespace filters by the namespace of the objects
	Namespace string `protobuf:"bytes,2,opt,name=namespace"`
}

func (r *ConfigFilter) Filter(ctx context.Context, obj runtime.Object) bool {
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
func (r *Config) ValidateCreate(ctx context.Context) field.ErrorList {
	var allErrs field.ErrorList
	log := log.FromContext(ctx)
	accessor, err := meta.Accessor(r)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath(""),
			r,
			err.Error(),
		))
		return allErrs
	}
	log.Info("validate create", "name", accessor.GetName(), "resourceVersion", accessor.GetResourceVersion())
	if _, err := GetTargetKey(accessor.GetLabels()); err != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("metadata.labels"),
			r,
			err.Error(),
		))
	}
	return allErrs
}

func (r *Config) ValidateUpdate(ctx context.Context, old runtime.Object) field.ErrorList {
	var allErrs field.ErrorList

	newaccessor, err := meta.Accessor(r)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath(""),
			r,
			err.Error(),
		))
		return allErrs
	}
	newKey, err := GetTargetKey(newaccessor.GetLabels())
	if err != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("metadata.labels"),
			r,
			err.Error(),
		))
	}
	oldaccessor, err := meta.Accessor(old)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath(""),
			r,
			err.Error(),
		))
		return allErrs
	}
	oldKey, err := GetTargetKey(oldaccessor.GetLabels())
	if err != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("metadata.labels"),
			r,
			err.Error(),
		))
	}

	if len(allErrs) != 0 {
		return allErrs
	}
	if oldKey.String() != newKey.String() {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("metadata.labels"),
			r,
			fmt.Errorf("target keys are immutable: oldKey: %s, newKey %s", oldKey.String(), newKey.String()).Error(),
		))
	}

	return allErrs
}
