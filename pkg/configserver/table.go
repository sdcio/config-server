// Copyright 2023 The xxx Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package configserver

import (
	"context"
	"fmt"
	"net/http"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type tableConvertor struct {
	resource schema.GroupResource
	// cells creates a single row of cells of the table from a runtime.Object
	cells func(obj runtime.Object) []interface{}
	// columns stores column definitions for the table convertor
	columns []metav1.TableColumnDefinition
}

// ConvertToTable implements rest.TableConvertor
func (c tableConvertor) ConvertToTable(ctx context.Context, object runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
	var table metav1.Table

	fn := func(obj runtime.Object) error {
		obj = obj
		cells := c.cells(obj)
		if len(cells) == 0 {
			return errNotAcceptable{resource: c.resource}
		}
		table.Rows = append(table.Rows, metav1.TableRow{
			Cells:  cells,
			Object: runtime.RawExtension{Object: obj},
		})
		return nil
	}

	// Create table rows
	switch {
	case meta.IsListType(object):
		if err := meta.EachListItem(object, fn); err != nil {
			return nil, err
		}
	default:
		if err := fn(object); err != nil {
			return nil, err
		}
	}

	// Populate table metadata
	table.APIVersion = metav1.SchemeGroupVersion.Identifier()
	table.Kind = "Table"
	if l, err := meta.ListAccessor(object); err == nil {
		table.ResourceVersion = l.GetResourceVersion()
		table.SelfLink = l.GetSelfLink()
		table.Continue = l.GetContinue()
		table.RemainingItemCount = l.GetRemainingItemCount()
	} else if c, err := meta.CommonAccessor(object); err == nil {
		table.ResourceVersion = c.GetResourceVersion()
		//table.SelfLink = c.GetSelfLink()
	}
	if opt, ok := tableOptions.(*metav1.TableOptions); !ok || !opt.NoHeaders {
		table.ColumnDefinitions = c.columns
	}

	return &table, nil
}

// errNotAcceptable indicates the resource doesn't support Table conversion
type errNotAcceptable struct {
	resource schema.GroupResource
}

func (e errNotAcceptable) Error() string {
	return fmt.Sprintf("the resource %s does not support being converted to a Table", e.resource)
}

func (e errNotAcceptable) Status() metav1.Status {
	return metav1.Status{
		Status:  metav1.StatusFailure,
		Code:    http.StatusNotAcceptable,
		Reason:  metav1.StatusReason("NotAcceptable"),
		Message: e.Error(),
	}
}
