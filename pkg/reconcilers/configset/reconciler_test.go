package configset

import (
	"context"
	"testing"

	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestBuildConfigPropagatesDeletePaths(t *testing.T) {
	cs := &configv1alpha1.ConfigSet{
		ObjectMeta: metav1.ObjectMeta{Name: "cs", Namespace: "ns"},
		Spec:       configv1alpha1.ConfigSetSpec{Priority: 5, DeletePaths: []string{"/interfaces"}},
	}
	cfg := buildConfig(context.Background(), cs, types.NamespacedName{Namespace: "ns", Name: "t1"})
	if len(cfg.Spec.DeletePaths) != 1 || cfg.Spec.DeletePaths[0] != "/interfaces" || len(cfg.Spec.Config) != 0 {
		t.Fatalf("spec = %+v", cfg.Spec)
	}
	if got := buildConfigSpec(cs); len(got.DeletePaths) != 1 {
		t.Fatalf("update spec = %+v", got)
	}
}
