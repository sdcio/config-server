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
	"strings"

	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	"github.com/henderiw/apiserver-store/pkg/generic/registry"
	"github.com/sdcio/config-server/apis/condition"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
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
var _ resource.InternalObject = &Config{}
var _ resource.ObjectList = &ConfigList{}
var _ resource.ObjectWithStatusSubResource = &Config{}
var _ resource.StatusSubResource = &ConfigStatus{}

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

// IsEqual returns a bool indicating if the desired state of both resources is equal or not
func (r *Config) IsEqual(ctx context.Context, obj, old runtime.Object) bool {
	newobj := obj.(*Config)
	oldobj := old.(*Config)

	if !apiequality.Semantic.DeepEqual(oldobj.ObjectMeta, newobj.ObjectMeta) {
		return false
	}
	// if equal we also test the spec
	return apiequality.Semantic.DeepEqual(oldobj.Spec, newobj.Spec)
}

// GetStatus return the resource.StatusSubResource interface
func (r *Config) GetStatus() resource.StatusSubResource {
	return r.Status
}

// IsStatusEqual returns a bool indicating if the status of both resources is equal or not
func (r *Config) IsStatusEqual(ctx context.Context, obj, old runtime.Object) bool {
	newobj := obj.(*Config)
	oldobj := old.(*Config)
	return apiequality.Semantic.DeepEqual(oldobj.Status, newobj.Status)
}

// PrepareForStatusUpdate prepares the status update
func (r *Config) PrepareForStatusUpdate(ctx context.Context, obj, old runtime.Object) {
	newObj := obj.(*Config)
	oldObj := old.(*Config)
	newObj.Spec = oldObj.Spec

	// Status updates are for only for updating status, not objectmeta.
	metav1.ResetObjectMetaForStatus(&newObj.ObjectMeta, &newObj.ObjectMeta)
}

// ValidateStatusUpdate validates status updates
func (r *Config) ValidateStatusUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	var allErrs field.ErrorList
	return allErrs
}

// SubResourceName resturns the name of the subresource
// SubResourceName implements the resource.StatusSubResource
func (ConfigStatus) SubResourceName() string {
	return fmt.Sprintf("%s/%s", ConfigPlural, "status")
}

// CopyTo copies the content of the status subresource to a parent resource.
// CopyTo implements the resource.StatusSubResource
func (r ConfigStatus) CopyTo(obj resource.ObjectWithStatusSubResource) {
	parent, ok := obj.(*Config)
	if ok {
		parent.Status = r
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
					fmt.Sprintf("%s.%s/%s", strings.ToLower(ConfigKind), GroupName, config.Name),
					config.GetCondition(condition.ConditionTypeReady).Status,
					config.GetCondition(condition.ConditionTypeReady).Reason,
					config.Spec.Priority,
					config.GetTarget(),
					config.GetLastKnownGoodSchema().FileString(),
				}
			},
			[]metav1.TableColumnDefinition{
				{Name: "Name", Type: "string"},
				{Name: "Ready", Type: "string"},
				{Name: "Reason", Type: "string"},
				{Name: "Priority", Type: "integer"},
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
		filter := &ConfigFilter{}

		if fieldSelector != nil {
			for _, requirement := range fieldSelector.Requirements() {
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
		}
		// add namespace to the filter selector if specified
		namespace, ok := genericapirequest.NamespaceFrom(ctx)
		if ok {
			if filter.Namespace == "" {
				filter.Namespace = namespace
			}
		}

		return filter, nil
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

func (r *Config) PrepareForCreate(ctx context.Context, obj runtime.Object) {
	// status cannot be set upon create -> reset it
	newobj := obj.(*Config)
	newobj.Status = ConfigStatus{}
}

// ValidateCreate statically validates
func (r *Config) ValidateCreate(ctx context.Context, obj runtime.Object) field.ErrorList {
	var allErrs field.ErrorList
	accessor, err := meta.Accessor(obj)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath(""),
			obj,
			err.Error(),
		))
		return allErrs
	}
	if _, err := GetTargetKey(accessor.GetLabels()); err != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("metadata.labels"),
			obj,
			err.Error(),
		))
	}

	newobj := obj.(*Config)
	if newobj.Spec.Priority < 2 {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec.priority"),
			obj,
			"priority should be bigger 2",
		))
	}

	return allErrs
}

func (r *Config) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
	// ensure the sttaus dont get updated
	newobj := obj.(*Config)
	oldObj := old.(*Config)
	newobj.Status = oldObj.Status
}

func (r *Config) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	var allErrs field.ErrorList

	newaccessor, err := meta.Accessor(obj)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath(""),
			obj,
			err.Error(),
		))
		return allErrs
	}
	newKey, err := GetTargetKey(newaccessor.GetLabels())
	if err != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("metadata.labels"),
			obj,
			err.Error(),
		))
	}
	oldaccessor, err := meta.Accessor(old)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath(""),
			obj,
			err.Error(),
		))
		return allErrs
	}
	oldKey, err := GetTargetKey(oldaccessor.GetLabels())
	if err != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("metadata.labels"),
			obj,
			err.Error(),
		))
	}

	if len(allErrs) != 0 {
		return allErrs
	}
	if oldKey.String() != newKey.String() {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("metadata.labels"),
			obj,
			fmt.Errorf("target keys are immutable: oldKey: %s, newKey %s", oldKey.String(), newKey.String()).Error(),
		))
	}
	return allErrs
}
