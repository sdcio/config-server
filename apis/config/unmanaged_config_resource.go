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
	apiequality "k8s.io/apimachinery/pkg/api/equality"
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
	UnManagedConfigPlural   = "unmanagedconfigs"
	UnManagedConfigSingular = "unmanagedconfig"
)

var (
	UnManagedConfigShortNames = []string{}
	UnManagedConfigCategories = []string{"sdc", "knet"}
)

// +k8s:deepcopy-gen=false
var _ resource.InternalObject = &UnManagedConfig{}
var _ resource.ObjectList = &UnManagedConfigList{}
var _ resource.ObjectWithStatusSubResource = &UnManagedConfig{}
var _ resource.StatusSubResource = &UnManagedConfigStatus{}

func (UnManagedConfig) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    SchemeGroupVersion.Group,
		Version:  SchemeGroupVersion.Version,
		Resource: UnManagedConfigPlural,
	}
}

// IsStorageVersion returns true -- UnManagedConfig is used as the internal version.
// IsStorageVersion implements resource.Object
func (UnManagedConfig) IsStorageVersion() bool {
	return true
}

// NamespaceScoped returns if UnManagedConfig is a namespaced resource.
// NamespaceScoped implements resource.Object
func (UnManagedConfig) NamespaceScoped() bool {
	return true
}

// GetObjectMeta implements resource.Object
// GetObjectMeta implements resource.Object
func (r *UnManagedConfig) GetObjectMeta() *metav1.ObjectMeta {
	return &r.ObjectMeta
}

// GetSingularName returns the singular name of the resource
// GetSingularName implements resource.Object
func (UnManagedConfig) GetSingularName() string {
	return UnManagedConfigSingular
}

// GetShortNames returns the shortnames for the resource
// GetShortNames implements resource.Object
func (UnManagedConfig) GetShortNames() []string {
	return UnManagedConfigShortNames
}

// GetCategories return the categories of the resource
// GetCategories implements resource.Object
func (UnManagedConfig) GetCategories() []string {
	return UnManagedConfigCategories
}

// New return an empty resource
// New implements resource.Object
func (UnManagedConfig) New() runtime.Object {
	return &UnManagedConfig{}
}

// NewList return an empty resourceList
// NewList implements resource.Object
func (UnManagedConfig) NewList() runtime.Object {
	return &UnManagedConfigList{}
}

// IsEqual returns a bool indicating if the desired state of both resources is equal or not
func (r *UnManagedConfig) IsEqual(ctx context.Context, obj, old runtime.Object) bool {
	newobj := obj.(*UnManagedConfig)
	oldobj := old.(*UnManagedConfig)
	if !apiequality.Semantic.DeepEqual(oldobj.ObjectMeta, newobj.ObjectMeta) {
		return false
	}
	return apiequality.Semantic.DeepEqual(oldobj.Spec, newobj.Spec)
}

// GetStatus return the resource.StatusSubResource interface
func (r *UnManagedConfig) GetStatus() resource.StatusSubResource {
	return r.Status
}

// IsStatusEqual returns a bool indicating if the status of both resources is equal or not
func (r *UnManagedConfig) IsStatusEqual(ctx context.Context, obj, old runtime.Object) bool {
	newobj := obj.(*UnManagedConfig)
	oldobj := old.(*UnManagedConfig)
	return apiequality.Semantic.DeepEqual(oldobj.Status, newobj.Status)
}

// PrepareForStatusUpdate prepares the status update
func (r *UnManagedConfig) PrepareForStatusUpdate(ctx context.Context, obj, old runtime.Object) {
	newObj := obj.(*UnManagedConfig)
	oldObj := old.(*UnManagedConfig)
	newObj.Spec = oldObj.Spec

	// Status updates are for only for updating status, not objectmeta.
	metav1.ResetObjectMetaForStatus(&newObj.ObjectMeta, &newObj.ObjectMeta)
}

// ValidateStatusUpdate validates status updates
func (r *UnManagedConfig) ValidateStatusUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	var allErrs field.ErrorList
	return allErrs
}

// SubResourceName resturns the name of the subresource
// SubResourceName implements the resource.StatusSubResource
func (UnManagedConfigStatus) SubResourceName() string {
	return fmt.Sprintf("%s/%s", UnManagedConfigPlural, "status")
}

// CopyTo copies the content of the status subresource to a parent resource.
// CopyTo implements the resource.StatusSubResource
func (r UnManagedConfigStatus) CopyTo(obj resource.ObjectWithStatusSubResource) {
	cfg, ok := obj.(*UnManagedConfig)
	if ok {
		cfg.Status = r
	}
}

// GetListMeta returns the ListMeta
// GetListMeta implements the resource.ObjectList
func (r *UnManagedConfigList) GetListMeta() *metav1.ListMeta {
	return &r.ListMeta
}

// TableConvertor return the table format of the resource
func (r *UnManagedConfig) TableConvertor() func(gr schema.GroupResource) rest.TableConvertor {
	return func(gr schema.GroupResource) rest.TableConvertor {
		return registry.NewTableConverter(
			gr,
			func(obj runtime.Object) []interface{} {
				UnManagedConfig, ok := obj.(*UnManagedConfig)
				if !ok {
					return nil
				}
				return []interface{}{
					fmt.Sprintf("%s.%s/%s", strings.ToLower(UnManagedConfigKind), GroupName, UnManagedConfig.Name),
					len(UnManagedConfig.Status.Deviations),
				}
			},
			[]metav1.TableColumnDefinition{
				{Name: "Name", Type: "string"},
				{Name: "Deviations", Type: "integer"},
			},
		)
	}
}

// FieldLabelConversion is the schema conversion function for normalizing the FieldSelector for the resource
func (r *UnManagedConfig) FieldLabelConversion() runtime.FieldLabelConversionFunc {
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

func (r *UnManagedConfig) FieldSelector() func(ctx context.Context, fieldSelector fields.Selector) (resource.Filter, error) {
	return func(ctx context.Context, fieldSelector fields.Selector) (resource.Filter, error) {
		filter := &UnManagedConfigFilter{}

		if fieldSelector != nil {
			for _, requirement := range fieldSelector.Requirements() {
				switch requirement.Operator {
				case selection.Equals, selection.DoesNotExist:
					if requirement.Value == "" {
						return filter, apierrors.NewBadRequest(fmt.Sprintf(
							"unsupported fieldSelector value %q for field %q with operator %q",
							requirement.Value, requirement.Field, requirement.Operator))
					}
				default:
					return filter, apierrors.NewBadRequest(fmt.Sprintf(
						"unsupported fieldSelector operator %q for field %q",
						requirement.Operator, requirement.Field))
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

type UnManagedConfigFilter struct {
	// Name filters by the name of the objects
	Name string `protobuf:"bytes,1,opt,name=name"`

	// Namespace filters by the namespace of the objects
	Namespace string `protobuf:"bytes,2,opt,name=namespace"`
}

func (r *UnManagedConfigFilter) Filter(ctx context.Context, obj runtime.Object) bool {
	f := false // result of the previous filter
	o, ok := obj.(*UnManagedConfig)
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

func (r *UnManagedConfig) PrepareForCreate(ctx context.Context, obj runtime.Object) {
	// status cannot be set upon create -> reset it
	newobj := obj.(*UnManagedConfig)
	newobj.Status = UnManagedConfigStatus{}
}

// ValidateCreate statically validates
func (r *UnManagedConfig) ValidateCreate(ctx context.Context, obj runtime.Object) field.ErrorList {
	var allErrs field.ErrorList
	return allErrs
}

func (r *UnManagedConfig) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
	// ensure the sttaus dont get updated
	newobj := obj.(*UnManagedConfig)
	oldObj := old.(*UnManagedConfig)
	newobj.Status = oldObj.Status
}

func (r *UnManagedConfig) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	var allErrs field.ErrorList
	return allErrs
}
