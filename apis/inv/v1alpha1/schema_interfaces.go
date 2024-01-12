/*
Copyright 2023 The Nephio Authors.

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

	sdcpb "github.com/iptecharch/sdc-protos/sdcpb"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetCondition returns the condition based on the condition kind
func (r *Schema) GetCondition(t ConditionType) Condition {
	return r.Status.GetCondition(t)
}

// SetConditions sets the conditions on the resource. it allows for 0, 1 or more conditions
// to be set at once
func (r *Schema) SetConditions(c ...Condition) {
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
	name, vendor := GetVendorType(r.Provider)
	return &sdcpb.Schema{
		//Name: r.Provider,
		Name:    name,
		Vendor:  vendor,
		Version: r.Version,
	}
}

func (r *SchemaSpec) GetNewSchemaBase(basePath string) SchemaSpecSchema {
	basePath = r.GetBasePath(basePath)

	// when no models are supplied we use the base dir
	models := initSlice(r.Schema.Models, ".")
	includes := initSlice(r.Schema.Includes, "")
	excludes := initSlice(r.Schema.Excludes, "")

	return SchemaSpecSchema{
		Models:   getNewBase(basePath, models),
		Includes: getNewBase(basePath, includes),
		Excludes: excludes,
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
