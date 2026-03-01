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
	DeviationPlural   = "deviations"
	DeviationSingular = "deviation"
)

var (
	DeviationShortNames = []string{}
	DeviationCategories = []string{"sdc", "knet"}
)

// +k8s:deepcopy-gen=false
var _ resource.InternalObject = &Deviation{}
var _ resource.ObjectList = &DeviationList{}
var _ resource.ObjectWithStatusSubResource = &Deviation{}
var _ resource.StatusSubResource = &DeviationStatus{}

func (Deviation) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    SchemeGroupVersion.Group,
		Version:  SchemeGroupVersion.Version,
		Resource: DeviationPlural,
	}
}

// IsStorageVersion returns true -- Deviation is used as the internal version.
// IsStorageVersion implements resource.Object
func (Deviation) IsStorageVersion() bool {
	return true
}

// NamespaceScoped returns if Deviation is a namespaced resource.
// NamespaceScoped implements resource.Object
func (Deviation) NamespaceScoped() bool {
	return true
}

// GetObjectMeta implements resource.Object
// GetObjectMeta implements resource.Object
func (r *Deviation) GetObjectMeta() *metav1.ObjectMeta {
	return &r.ObjectMeta
}

// GetSingularName returns the singular name of the resource
// GetSingularName implements resource.Object
func (Deviation) GetSingularName() string {
	return DeviationSingular
}

// GetShortNames returns the shortnames for the resource
// GetShortNames implements resource.Object
func (Deviation) GetShortNames() []string {
	return DeviationShortNames
}

// GetCategories return the categories of the resource
// GetCategories implements resource.Object
func (Deviation) GetCategories() []string {
	return DeviationCategories
}

// New return an empty resource
// New implements resource.Object
func (Deviation) New() runtime.Object {
	return &Deviation{}
}

// NewList return an empty resourceList
// NewList implements resource.Object
func (Deviation) NewList() runtime.Object {
	return &DeviationList{}
}

// IsEqual returns a bool indicating if the desired state of both resources is equal or not
func (r *Deviation) IsEqual(ctx context.Context, obj, old runtime.Object) bool {
	newobj := obj.(*Deviation)
	oldobj := old.(*Deviation)
	if !apiequality.Semantic.DeepEqual(oldobj.ObjectMeta, newobj.ObjectMeta) {
		return false
	}
	return apiequality.Semantic.DeepEqual(oldobj.Spec, newobj.Spec)
}

// GetStatus return the resource.StatusSubResource interface
func (r *Deviation) GetStatus() resource.StatusSubResource {
	return r.Status
}

// IsStatusEqual returns a bool indicating if the status of both resources is equal or not
func (r *Deviation) IsStatusEqual(ctx context.Context, obj, old runtime.Object) bool {
	newobj := obj.(*Deviation)
	oldobj := old.(*Deviation)
	return apiequality.Semantic.DeepEqual(oldobj.Status, newobj.Status)
}

// PrepareForStatusUpdate prepares the status update
func (r *Deviation) PrepareForStatusUpdate(ctx context.Context, obj, old runtime.Object) {
	newObj := obj.(*Deviation)
	oldObj := old.(*Deviation)
	newObj.Spec = oldObj.Spec

	// Status updates are for only for updating status, not objectmeta.
	metav1.ResetObjectMetaForStatus(&newObj.ObjectMeta, &newObj.ObjectMeta)
}

// ValidateStatusUpdate validates status updates
func (r *Deviation) ValidateStatusUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	var allErrs field.ErrorList
	return allErrs
}

// SubResourceName resturns the name of the subresource
// SubResourceName implements the resource.StatusSubResource
func (DeviationStatus) SubResourceName() string {
	return fmt.Sprintf("%s/%s", DeviationPlural, "status")
}

// CopyTo copies the content of the status subresource to a parent resource.
// CopyTo implements the resource.StatusSubResource
func (r DeviationStatus) CopyTo(obj resource.ObjectWithStatusSubResource) {
	cfg, ok := obj.(*Deviation)
	if ok {
		cfg.Status = r
	}
}

// GetListMeta returns the ListMeta
// GetListMeta implements the resource.ObjectList
func (r *DeviationList) GetListMeta() *metav1.ListMeta {
	return &r.ListMeta
}

// TableConvertor return the table format of the resource
func (r *Deviation) TableConvertor() func(gr schema.GroupResource) rest.TableConvertor {
	return func(gr schema.GroupResource) rest.TableConvertor {
		return registry.NewTableConverter(
			gr,
			func(obj runtime.Object) []interface{} {
				deviation, ok := obj.(*Deviation)
				if !ok {
					return nil
				}
				return []interface{}{
					fmt.Sprintf("%s.%s/%s", strings.ToLower(DeviationKind), GroupName, deviation.Name),
					deviation.GetDeviationType(),
					deviation.GetTarget(),
					len(deviation.Spec.Deviations),
				}
			},
			[]metav1.TableColumnDefinition{
				{Name: "Name", Type: "string"},
				{Name: "Type", Type: "string"},
				{Name: "Target", Type: "string"},
				{Name: "Deviations", Type: "integer"},
			},
		)
	}
}

// FieldLabelConversion is the schema conversion function for normalizing the FieldSelector for the resource
func (r *Deviation) FieldLabelConversion() runtime.FieldLabelConversionFunc {
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

func (r *Deviation) FieldSelector() func(ctx context.Context, fieldSelector fields.Selector) (resource.Filter, error) {
	return func(ctx context.Context, fieldSelector fields.Selector) (resource.Filter, error) {
		filter := &DeviationFilter{}

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

type DeviationFilter struct {
	// Name filters by the name of the objects
	Name string `protobuf:"bytes,1,opt,name=name"`

	// Namespace filters by the namespace of the objects
	Namespace string `protobuf:"bytes,2,opt,name=namespace"`
}

func (r *DeviationFilter) Filter(ctx context.Context, obj runtime.Object) bool {
	o, ok := obj.(*Deviation)
	if !ok {
		return true
	}
	if r.Name != "" && o.GetName() != r.Name {
		return true
	}
	if r.Namespace != "" && o.GetNamespace() != r.Namespace {
		return true
	}
	return false
}

func (r *Deviation) PrepareForCreate(ctx context.Context, obj runtime.Object) {
	// status cannot be set upon create -> reset it
	newobj := obj.(*Deviation)
	newobj.Status = DeviationStatus{}
}

// ValidateCreate statically validates
func (r *Deviation) ValidateCreate(ctx context.Context, obj runtime.Object) field.ErrorList {
	var allErrs field.ErrorList
	return allErrs
}

func (r *Deviation) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
	// ensure the sttaus dont get updated
	newobj := obj.(*Deviation)
	oldObj := old.(*Deviation)
	newobj.Status = oldObj.Status
}

func (r *Deviation) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	var allErrs field.ErrorList
	return allErrs
}
