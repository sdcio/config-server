/*
Copyright 2026 Nokia.

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
	TargetSnapshotPlural   = "targetsnapshots"
	TargetSnapshotSingular = "targetsnapshot"
)

var (
	TargetSnapshotShortNames = []string{}
	TargetSnapshotCategories = []string{"sdc", "knet"}
)

// +k8s:deepcopy-gen=false
var _ resource.InternalObject = &TargetSnapshot{}
var _ resource.ObjectList = &TargetSnapshotList{}

func (TargetSnapshot) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    SchemeGroupVersion.Group,
		Version:  SchemeGroupVersion.Version,
		Resource: TargetSnapshotPlural,
	}
}

// IsStorageVersion returns true -- Config is used as the internal version.
// IsStorageVersion implements resource.Object
func (TargetSnapshot) IsStorageVersion() bool {
	return true
}

// NamespaceScoped returns true to indicate Fortune is a namespaced resource.
// NamespaceScoped implements resource.Object
func (TargetSnapshot) NamespaceScoped() bool {
	return true
}

// GetObjectMeta implements resource.Object
// GetObjectMeta implements resource.Object
func (r *TargetSnapshot) GetObjectMeta() *metav1.ObjectMeta {
	return &r.ObjectMeta
}

// GetSingularName returns the singular name of the resource
// GetSingularName implements resource.Object
func (TargetSnapshot) GetSingularName() string {
	return TargetSnapshotSingular
}

// GetShortNames returns the shortnames for the resource
// GetShortNames implements resource.Object
func (TargetSnapshot) GetShortNames() []string {
	return TargetSnapshotShortNames
}

// GetCategories return the categories of the resource
// GetCategories implements resource.Object
func (TargetSnapshot) GetCategories() []string {
	return TargetSnapshotCategories
}

// New return an empty resource
// New implements resource.Object
func (TargetSnapshot) New() runtime.Object {
	return &TargetSnapshot{}
}

// NewList return an empty resourceList
// NewList implements resource.Object
func (TargetSnapshot) NewList() runtime.Object {
	return &TargetSnapshotList{}
}

// IsEqual returns a bool indicating if the desired state of both resources is equal or not
func (r *TargetSnapshot) IsEqual(ctx context.Context, obj, old runtime.Object) bool {
	newobj := obj.(*TargetSnapshot)
	oldobj := old.(*TargetSnapshot)

	if !apiequality.Semantic.DeepEqual(oldobj.ObjectMeta, newobj.ObjectMeta) {
		return false
	}
	// if equal we also test the spec
	return apiequality.Semantic.DeepEqual(oldobj.Spec, newobj.Spec)
}

// GetListMeta returns the ListMeta
// GetListMeta implements the resource.ObjectList
func (r *TargetSnapshotList) GetListMeta() *metav1.ListMeta {
	return &r.ListMeta
}

// TableConvertor return the table format of the resource
func (r *TargetSnapshot) TableConvertor() func(gr schema.GroupResource) rest.TableConvertor {
	return func(gr schema.GroupResource) rest.TableConvertor {
		return registry.NewTableConverter(
			gr,
			func(obj runtime.Object) []interface{} {
				targetSnapshot, ok := obj.(*TargetSnapshot)
				if !ok {
					return nil
				}
				return []interface{}{
					fmt.Sprintf("%s.%s/%s", strings.ToLower(TargetSnapshotKind), GroupName, targetSnapshot.Name),
				}
			},
			[]metav1.TableColumnDefinition{
				{Name: "Name", Type: "string"},
			},
		)
	}
}

// FieldLabelConversion is the schema conversion function for normalizing the FieldSelector for the resource
func (r *TargetSnapshot) FieldLabelConversion() runtime.FieldLabelConversionFunc {
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

func (r *TargetSnapshot) FieldSelector() func(ctx context.Context, fieldSelector fields.Selector) (resource.Filter, error) {
	return func(ctx context.Context, fieldSelector fields.Selector) (resource.Filter, error) {
		filter := &TargetSnapshotFilter{}

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

type TargetSnapshotFilter struct {
	// Name filters by the name of the objects
	Name string `protobuf:"bytes,1,opt,name=name"`

	// Namespace filters by the namespace of the objects
	Namespace string `protobuf:"bytes,2,opt,name=namespace"`
}

func (r *TargetSnapshotFilter) Filter(ctx context.Context, obj runtime.Object) bool {
	o, ok := obj.(*TargetSnapshot)
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

func (r *TargetSnapshot) PrepareForCreate(ctx context.Context, obj runtime.Object) {
}

// ValidateCreate statically validates
func (r *TargetSnapshot) ValidateCreate(ctx context.Context, obj runtime.Object) field.ErrorList {
	var allErrs field.ErrorList
	return allErrs
}

func (r *TargetSnapshot) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
}

func (r *TargetSnapshot) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	var allErrs field.ErrorList
	return allErrs
}
