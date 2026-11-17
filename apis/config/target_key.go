package config

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// GetTargetKey returns the Target an object is bound to: the object's own
// namespace plus the name in its TargetNameKey label.
func GetTargetKey(obj metav1.Object) (types.NamespacedName, error) {
	name := obj.GetLabels()[TargetNameKey] // reading a nil map is safe
	ns := obj.GetNamespace()
	if name == "" || ns == "" {
		return types.NamespacedName{}, fmt.Errorf("target requires namespace and label %s, got %q/%q", TargetNameKey, ns, name)
	}
	return types.NamespacedName{Namespace: ns, Name: name}, nil
}

// GetTargetString renders "<namespace>/<target>" for table output.
// Returns "" when the object has no labels (same as the old GetTarget).
func GetTargetString(obj metav1.Object) string {
	labels := obj.GetLabels()
	if len(labels) == 0 {
		return ""
	}
	return obj.GetNamespace() + "/" + labels[TargetNameKey]
}

// ValidateTargetKey is the create-time check shared by all target-bound resources.
func ValidateTargetKey(obj runtime.Object) field.ErrorList {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return field.ErrorList{field.Invalid(field.NewPath(""), obj, err.Error())}
	}
	if _, err := GetTargetKey(accessor); err != nil {
		return field.ErrorList{field.Invalid(field.NewPath("metadata", "labels"), obj, err.Error())}
	}
	return nil
}

// ValidateTargetKeyUpdate checks both objects carry a valid key and that it did not change.
func ValidateTargetKeyUpdate(obj, old runtime.Object) field.ErrorList {
	if errs := append(ValidateTargetKey(obj), ValidateTargetKey(old)...); len(errs) != 0 {
		return errs
	}
	newKey, _ := GetTargetKey(obj.(metav1.Object))
	oldKey, _ := GetTargetKey(old.(metav1.Object))
	if newKey != oldKey {
		return field.ErrorList{field.Invalid(field.NewPath("metadata", "labels"), obj,
			fmt.Sprintf("target key is immutable: old %s, new %s", oldKey, newKey))}
	}
	return nil
}