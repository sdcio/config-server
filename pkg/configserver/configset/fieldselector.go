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

package configset

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/selection"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
)

// Filter
type Filter struct {
	// Name filters by the name of the objects
	Name string

	// Namespace filters by the namespace of the objects
	Namespace string
}

// parseFieldSelector parses client-provided fields.Selector into a packageFilter
func parseFieldSelector(ctx context.Context, fieldSelector fields.Selector) (*Filter, error) {
	var filter *Filter

	// add the namespace to the list
	namespace, ok := genericapirequest.NamespaceFrom(ctx)
	if fieldSelector == nil {
		if ok {
			return &Filter{Namespace: namespace}, nil
		}
		return filter, nil
	}

	requirements := fieldSelector.Requirements()
	for _, requirement := range requirements {
		filter = &Filter{}
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
			filter = &Filter{Namespace: namespace}
		}
	}

	return filter, nil
}
