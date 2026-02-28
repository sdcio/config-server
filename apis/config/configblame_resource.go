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
	ConfigBlamePlural   = "configblames"
	ConfigBlameSingular = "configblame"
)

var (
	ConfigBlameShortNames = []string{}
	ConfigBlameCategories = []string{"sdc", "knet"}
)

// +k8s:deepcopy-gen=false
var _ resource.InternalObject = &ConfigBlame{}
var _ resource.ObjectList = &ConfigBlameList{}

func (ConfigBlame) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    SchemeGroupVersion.Group,
		Version:  SchemeGroupVersion.Version,
		Resource: ConfigBlamePlural,
	}
}

// IsStorageVersion returns true -- ConfigBlame is used as the internal version.
// IsStorageVersion implements resource.Object
func (ConfigBlame) IsStorageVersion() bool {
	return true
}

// NamespaceScoped returns if ConfigBlame is a namespaced resource.
// NamespaceScoped implements resource.Object
func (ConfigBlame) NamespaceScoped() bool {
	return true
}

// GetObjectMeta implements resource.Object
// GetObjectMeta implements resource.Object
func (r *ConfigBlame) GetObjectMeta() *metav1.ObjectMeta {
	return &r.ObjectMeta
}

// GetSingularName returns the singular name of the resource
// GetSingularName implements resource.Object
func (ConfigBlame) GetSingularName() string {
	return ConfigBlameSingular
}

// GetShortNames returns the shortnames for the resource
// GetShortNames implements resource.Object
func (ConfigBlame) GetShortNames() []string {
	return ConfigBlameShortNames
}

// GetCategories return the categories of the resource
// GetCategories implements resource.Object
func (ConfigBlame) GetCategories() []string {
	return ConfigBlameCategories
}

// New return an empty resource
// New implements resource.Object
func (ConfigBlame) New() runtime.Object {
	return &ConfigBlame{}
}

// NewList return an empty resourceList
// NewList implements resource.Object
func (ConfigBlame) NewList() runtime.Object {
	return &ConfigBlameList{}
}

// IsEqual returns a bool indicating if the desired state of both resources is equal or not
func (r *ConfigBlame) IsEqual(ctx context.Context, obj, old runtime.Object) bool {
	newobj := obj.(*ConfigBlame)
	oldobj := old.(*ConfigBlame)
	return apiequality.Semantic.DeepEqual(oldobj.Spec, newobj.Spec)
}

// GetListMeta returns the ListMeta
// GetListMeta implements the resource.ObjectList
func (r *ConfigBlameList) GetListMeta() *metav1.ListMeta {
	return &r.ListMeta
}

// TableConvertor return the table format of the resource
func (r *ConfigBlame) TableConvertor() func(gr schema.GroupResource) rest.TableConvertor {
	return func(gr schema.GroupResource) rest.TableConvertor {
		return registry.NewTableConverter(
			gr,
			func(obj runtime.Object) []interface{} {
				ConfigBlame, ok := obj.(*ConfigBlame)
				if !ok {
					return nil
				}
				return []interface{}{
					fmt.Sprintf("%s.%s/%s", strings.ToLower(ConfigBlameKind), GroupName, ConfigBlame.Name),
				}
			},
			[]metav1.TableColumnDefinition{
				{Name: "Name", Type: "string"},
			},
		)
	}
}

// FieldLabelConversion is the schema conversion function for normalizing the FieldSelector for the resource
func (r *ConfigBlame) FieldLabelConversion() runtime.FieldLabelConversionFunc {
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

func (r *ConfigBlame) FieldSelector() func(ctx context.Context, fieldSelector fields.Selector) (resource.Filter, error) {
	return func(ctx context.Context, fieldSelector fields.Selector) (resource.Filter, error) {
		filter := &ConfigBlameFilter{}

		// add the namespace to the list

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

type ConfigBlameFilter struct {
	// Name filters by the name of the objects
	Name string `protobuf:"bytes,1,opt,name=name"`

	// Namespace filters by the namespace of the objects
	Namespace string `protobuf:"bytes,2,opt,name=namespace"`
}

func (r *ConfigBlameFilter) Filter(ctx context.Context, obj runtime.Object) bool {
	o, ok := obj.(*ConfigBlame)
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

func (r *ConfigBlame) PrepareForCreate(ctx context.Context, obj runtime.Object) {
}

// ValidateCreate statically validates
func (r *ConfigBlame) ValidateCreate(ctx context.Context, obj runtime.Object) field.ErrorList {
	var allErrs field.ErrorList
	return allErrs
}

func (r *ConfigBlame) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
}

func (r *ConfigBlame) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	var allErrs field.ErrorList
	return allErrs
}
