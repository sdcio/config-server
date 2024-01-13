package testhelper

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestGetObject(t *testing.T) {

	cases := map[string]struct {
		path        string
		addToScheme func(s *runtime.Scheme) error
		gv          schema.GroupVersion
		obj         runtime.Object
	}{
		/*
			"Config": {
				path:        "../../example/config/config.yaml",
				addToScheme: configv1alpha1.AddToScheme,
				gv:         configv1alpha1.ConfigGroupVersion,
				obj:         &configv1alpha1.Config{},
			},
		*/
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			gvk := tc.gv.WithKind(reflect.TypeOf(tc.obj).Name())
			obj := tc.obj
			if err := GetKRMResource(tc.path, obj, gvk, tc.addToScheme); err != nil {
				t.Errorf("%s unexpected error: %s", name, err.Error())
				return
			}
		})
	}
}
