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
	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/sdcio/config-server/apis/condition"
	"github.com/sdcio/sdc-protos/sdcpb"
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
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
)

const (
	TargetPlural   = "targets"
	TargetSingular = "target"
)

var (
	TargetShortNames = []string{}
	TargetCategories = []string{"sdc", "knet"}
)

// +k8s:deepcopy-gen=false
var _ resource.InternalObject = &Target{}
var _ resource.ObjectList = &TargetList{}
var _ resource.ObjectWithStatusSubResource = &Target{}
var _ resource.StatusSubResource = &TargetStatus{}

func (Target) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    SchemeGroupVersion.Group,
		Version:  SchemeGroupVersion.Version,
		Resource: TargetPlural,
	}
}

// IsStorageVersion returns true -- Target is used as the internal version.
// IsStorageVersion implements resource.Object
func (Target) IsStorageVersion() bool {
	return true
}

// NamespaceScoped returns true to indicate Fortune is a namespaced resource.
// NamespaceScoped implements resource.Object
func (Target) NamespaceScoped() bool {
	return true
}

// GetObjectMeta implements resource.Object
// GetObjectMeta implements resource.Object
func (r *Target) GetObjectMeta() *metav1.ObjectMeta {
	return &r.ObjectMeta
}

// GetSingularName returns the singular name of the resource
// GetSingularName implements resource.Object
func (Target) GetSingularName() string {
	return TargetSingular
}

// GetShortNames returns the shortnames for the resource
// GetShortNames implements resource.Object
func (Target) GetShortNames() []string {
	return TargetShortNames
}

// GetCategories return the categories of the resource
// GetCategories implements resource.Object
func (Target) GetCategories() []string {
	return TargetCategories
}

// New return an empty resource
// New implements resource.Object
func (Target) New() runtime.Object {
	return &Target{}
}

// NewList return an empty resourceList
// NewList implements resource.Object
func (Target) NewList() runtime.Object {
	return &TargetList{}
}

// IsEqual returns a bool indicating if the desired state of both resources is equal or not
func (r *Target) IsEqual(ctx context.Context, obj, old runtime.Object) bool {
	newobj := obj.(*Target)
	oldobj := old.(*Target)

	if !apiequality.Semantic.DeepEqual(oldobj.ObjectMeta, newobj.ObjectMeta) {
		return false
	}
	// if equal we also test the spec
	return apiequality.Semantic.DeepEqual(oldobj.Spec, newobj.Spec)
}

// GetStatus return the resource.StatusSubResource interface
func (r *Target) GetStatus() resource.StatusSubResource {
	return r.Status
}

// IsStatusEqual returns a bool indicating if the status of both resources is equal or not
func (r *Target) IsStatusEqual(ctx context.Context, obj, old runtime.Object) bool {
	newobj := obj.(*Target)
	oldobj := old.(*Target)
	return apiequality.Semantic.DeepEqual(oldobj.Status, newobj.Status)
}

// PrepareForStatusUpdate prepares the status update
func (r *Target) PrepareForStatusUpdate(ctx context.Context, obj, old runtime.Object) {
	newObj := obj.(*Target)
	oldObj := old.(*Target)
	newObj.Spec = oldObj.Spec

	// Status updates are for only for updating status, not objectmeta.
	metav1.ResetObjectMetaForStatus(&newObj.ObjectMeta, &newObj.ObjectMeta)
}

// ValidateStatusUpdate validates status updates
func (r *Target) ValidateStatusUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	var allErrs field.ErrorList
	return allErrs
}

// SubResourceName resturns the name of the subresource
// SubResourceName implements the resource.StatusSubResource
func (TargetStatus) SubResourceName() string {
	return fmt.Sprintf("%s/%s", TargetPlural, "status")
}

// CopyTo copies the content of the status subresource to a parent resource.
// CopyTo implements the resource.StatusSubResource
func (r TargetStatus) CopyTo(obj resource.ObjectWithStatusSubResource) {
	parent, ok := obj.(*Target)
	if ok {
		parent.Status = r
	}
}

// GetListMeta returns the ListMeta
// GetListMeta implements the resource.ObjectList
func (r *TargetList) GetListMeta() *metav1.ListMeta {
	return &r.ListMeta
}

// TableConvertor return the table format of the resource
func (r *Target) TableConvertor() func(gr schema.GroupResource) rest.TableConvertor {
	return func(gr schema.GroupResource) rest.TableConvertor {
		return registry.NewTableConverter(
			gr,
			func(obj runtime.Object) []interface{} {
				target, ok := obj.(*Target)
				if !ok {
					return nil
				}
				d := target.Status.GetDiscoveryInfo()
				return []interface{}{
					fmt.Sprintf("%s.%s/%s", strings.ToLower(TargetKind), GroupName, target.Name),
					target.GetCondition(condition.ConditionTypeReady).Status,
					target.GetCondition(condition.ConditionTypeReady).Message,
					target.Spec.Provider,
					d.Version,
					target.Spec.Address,
					d.Platform,
					d.SerialNumber,
					d.MacAddress,
				}
			},
			[]metav1.TableColumnDefinition{
				{Name: "NAME", Type: "string"},
				{Name: "READY", Type: "string"},
				{Name: "REASON", Type: "string"},
				{Name: "PROVIDER", Type: "string"},
				{Name: "VERSION", Type: "string"},
				{Name: "ADDRESS", Type: "string"},
				{Name: "PLATFORM", Type: "string"},
				{Name: "SERIALNUMBER", Type: "string"},
				{Name: "MACADDRESS", Type: "string"},
			},
		)
	}
}

// FieldLabelConversion is the schema conversion function for normalizing the FieldSelector for the resource
func (r *Target) FieldLabelConversion() runtime.FieldLabelConversionFunc {
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

func (r *Target) FieldSelector() func(ctx context.Context, fieldSelector fields.Selector) (resource.Filter, error) {
	return func(ctx context.Context, fieldSelector fields.Selector) (resource.Filter, error) {
		filter := &TargetFilter{}

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

type TargetFilter struct {
	// Name filters by the name of the objects
	Name string `protobuf:"bytes,1,opt,name=name"`

	// Namespace filters by the namespace of the objects
	Namespace string `protobuf:"bytes,2,opt,name=namespace"`
}

func (r *TargetFilter) Filter(ctx context.Context, obj runtime.Object) bool {
	o, ok := obj.(*Target)
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

func (r *Target) PrepareForCreate(ctx context.Context, obj runtime.Object) {
	// status cannot be set upon create -> reset it
	newobj := obj.(*Target)
	newobj.Status = TargetStatus{}
}

// ValidateCreate statically validates
func (r *Target) ValidateCreate(ctx context.Context, obj runtime.Object) field.ErrorList {
	var allErrs field.ErrorList

	return allErrs
}

func (r *Target) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
	// ensure the status dont get updated
	newobj := obj.(*Target)
	oldObj := old.(*Target)
	newobj.Status = oldObj.Status
}

func (r *Target) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	var allErrs field.ErrorList

	return allErrs
}

var _ resource.ObjectWithArbitrarySubResource = &Target{}

func (r *Target) GetArbitrarySubResources() []resource.ArbitrarySubResource {
	return []resource.ArbitrarySubResource{
		&TargetRunning{},
	}
}

func (TargetRunning) SubResourceName() string {
	return "running"
}

func (TargetRunning) New() runtime.Object {
	return &Target{} // returns parent type — GET returns the full Target
}

func (TargetRunning) NewStorage(scheme *runtime.Scheme, parentStorage rest.Storage) (rest.Storage, error) {
	return &targetRunningREST{
		parentStore: parentStorage,
	}, nil
}

// targetRunningREST implements rest.Storage + rest.Getter
type targetRunningREST struct {
	parentStore rest.Storage
}

func (r *targetRunningREST) New() runtime.Object {
	return &Target{}
}

func (r *targetRunningREST) Destroy() {}

func (r *targetRunningREST) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	// Get the parent Target from the parent store
	getter := r.parentStore.(rest.Getter)
	obj, err := getter.Get(ctx, name, options)
	if err != nil {
		return nil, err
	}
	target := obj.(*Target)

	if !target.IsReady() {
		return nil, apierrors.NewNotFound(target.GetGroupVersionResource().GroupResource(), name)
	}

	cfg := &dsclient.Config{
		Address:  dsclient.GetDataServerAddress(),
		Insecure: true,
	}

	dsclient, closeFn, err := dsclient.NewEphemeral(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := closeFn(); err != nil {
			// You can use your preferred logging framework here
			fmt.Printf("failed to close connection: %v\n", err)
		}
	}()

	// check if the schema exists; this is == nil check; in case of err it does not exist
	key := target.GetNamespacedName()
	rsp, err := dsclient.GetIntent(ctx, &sdcpb.GetIntentRequest{
		DatastoreName: storebackend.KeyFromNSN(key).String(),
		Intent:        "running",
		Format:        sdcpb.Format_Intent_Format_JSON,
	})
	if err != nil {
		return nil, err
	}

	target.Running.Value.Raw = rsp.GetBlob()

	return target, nil
}
