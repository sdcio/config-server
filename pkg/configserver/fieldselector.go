package configserver

import (
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/selection"
)

// filter list the objects to filter on
type configFilter struct {
	// Name filters by the name of the objects
	Name string

	// Namespace filters by the namespace of the objects
	Namespace string

	// Repository filters to repositories with the given name.
	Target string

	// Package filters to packages with the given name.
	TargetNamespace string
}

// parseFieldSelector parses client-provided fields.Selector into a packageFilter
func parseFieldSelector(fieldSelector fields.Selector) (*configFilter, error) {
	var filter *configFilter

	if fieldSelector == nil {
		return filter, nil
	}

	requirements := fieldSelector.Requirements()
	for _, requirement := range requirements {
		filter = &configFilter{}
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

	return filter, nil
}
