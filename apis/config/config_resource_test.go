package config

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func testConfig(spec ConfigSpec) *Config {
	spec.Priority = 10
	return &Config{
		ObjectMeta: metav1.ObjectMeta{
			Name: "c",
			Labels: map[string]string{
				TargetNameKey:      "t",
				TargetNamespaceKey: "ns",
			},
		},
		Spec: spec,
	}
}

func TestConfigAdmission(t *testing.T) {
	blob := []ConfigBlob{{Path: "/a", Value: runtime.RawExtension{Raw: []byte(`{}`)}}}
	tests := []struct {
		name    string
		spec    ConfigSpec
		wantErr string
	}{
		{"content", ConfigSpec{Config: blob}, ""},
		{"sweeper", ConfigSpec{DeletePaths: []string{"/interfaces", "/"}}, ""},
		{"both", ConfigSpec{Config: blob, DeletePaths: []string{"/a"}}, "spec.deletePaths"},
		{"neither", ConfigSpec{}, "spec.config"},
		{"bad path", ConfigSpec{DeletePaths: []string{"/ok", "/a/[b"}}, "spec.deletePaths[1]"},
	}
	for _, tt := range tests {
		check := func(t *testing.T, errs []string) {
			t.Helper()
			if tt.wantErr == "" {
				if len(errs) != 0 {
					t.Fatalf("unexpected errors: %v", errs)
				}
				return
			}
			if !strings.Contains(strings.Join(errs, ";"), tt.wantErr) {
				t.Fatalf("errors %v do not mention %q", errs, tt.wantErr)
			}
		}
		t.Run("create/"+tt.name, func(t *testing.T) {
			var out []string
			for _, e := range (&Config{}).ValidateCreate(context.Background(), testConfig(tt.spec)) {
				out = append(out, e.Error())
			}
			check(t, out)
		})
		t.Run("update/"+tt.name, func(t *testing.T) {
			var out []string
			old := testConfig(ConfigSpec{Config: blob})
			for _, e := range (&Config{}).ValidateUpdate(context.Background(), testConfig(tt.spec), old) {
				out = append(out, e.Error())
			}
			check(t, out)
		})
	}
}
