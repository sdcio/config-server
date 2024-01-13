package testhelper

import (
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
)

func GetKRMResource(path string, obj runtime.Object, gvk schema.GroupVersionKind, addToScheme func(s *runtime.Scheme) error) error {
	// build scheme
	scheme, err := getScheme(runtime.SchemeBuilder{addToScheme})
	if err != nil {
		return err
	}
	c := &codec{
		scheme: scheme,
		gvk:    gvk,
	}

	if err := c.getObject(path, obj); err != nil {
		return err
	}
	return nil
}

func getScheme(sb runtime.SchemeBuilder) (*runtime.Scheme, error) {
	scheme := runtime.NewScheme()
	for _, addscheme := range sb {
		if err := addscheme(scheme); err != nil {
			return nil, err
		}
	}
	return scheme, nil
}

type codec struct {
	scheme *runtime.Scheme
	gvk    schema.GroupVersionKind
}

func (r *codec) getObject(path string, obj runtime.Object) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	s := json.NewYAMLSerializer(json.DefaultMetaFactory, r.scheme, r.scheme)
	_, _, err = s.Decode(b, &schema.GroupVersionKind{}, obj)
	return err
}
