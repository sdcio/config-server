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

package v1alpha1

import (
	"fmt"
	"path"
	"reflect"

	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	"github.com/sdcio/config-server/pkg/testhelper"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetCondition returns the condition based on the condition kind
func (r *Schema) GetCondition(t condv1alpha1.ConditionType) condv1alpha1.Condition {
	return r.Status.GetCondition(t)
}

// SetConditions sets the conditions on the resource. it allows for 0, 1 or more conditions
// to be set at once
func (r *Schema) SetConditions(c ...condv1alpha1.Condition) {
	r.Status.SetConditions(c...)
}

func (r *SchemaList) GetItems() []client.Object {
	objs := []client.Object{}
	for _, r := range r.Items {
		objs = append(objs, &r)
	}
	return objs
}

func (r *SchemaSpec) GetBasePath(baseDir string) string {
	return path.Join(baseDir, r.Provider, r.Version)
}

func (r *SchemaSpec) GetKey() string {
	return fmt.Sprintf("%s.%s", r.Provider, r.Version)
}

func (r *SchemaSpec) GetSchema() *sdcpb.Schema {
	return &sdcpb.Schema{
		Name:    "",
		Vendor:  r.Provider,
		Version: r.Version,
	}
}

func (r *SchemaSpec) GetNewSchemaBase(basePath string) SchemaSpecSchema {
	basePath = r.GetBasePath(basePath)

	// when no models are supplied we use the base dir
	modelsSet := sets.New[string]()
	includesSet := sets.New[string]()
	excludesSet := sets.New[string]()
	for _, repo := range r.Repositories {
		mergeSetWithSlice(modelsSet, repo.Schema.Models)
		mergeSetWithSlice(includesSet, repo.Schema.Includes)
		mergeSetWithSlice(excludesSet, repo.Schema.Excludes)
	}
	models := initSlice(modelsSet.UnsortedList(), ".")
	includes := initSlice(includesSet.UnsortedList(), "")
	excludes := initSlice(excludesSet.UnsortedList(), "")

	return SchemaSpecSchema{
		Models:   getNewBase(basePath, models),
		Includes: getNewBase(basePath, includes),
		Excludes: excludes,
	}
}

func mergeSetWithSlice(set sets.Set[string], newSlice []string) {
	for _, item := range newSlice {
		if !set.Has(item) {
			sets.Insert(set, item)
		}
	}
}

func initSlice(in []string, init string) []string {
	if len(in) == 0 {
		if init != "" {
			return []string{init}
		} else {
			return []string{}
		}
	}
	return in
}

func getNewBase(basePath string, in []string) []string {
	str := make([]string, 0, len(in))
	for _, s := range in {
		str = append(str, path.Join(basePath, s))
	}
	return str
}

// GetSchemaFromFile is a helper for tests to use the
// examples and validate them in unit tests
func GetSchemaFromFile(path string) (*DiscoveryRule, error) {
	addToScheme := AddToScheme
	obj := &DiscoveryRule{}
	gvk := SchemeGroupVersion.WithKind(reflect.TypeOf(obj).Name())
	// build object from file
	if err := testhelper.GetKRMResource(path, obj, gvk, addToScheme); err != nil {
		return nil, err
	}
	return obj, nil
}
