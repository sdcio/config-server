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

func TestConfigSetAdmission(t *testing.T) {
	blob := []ConfigBlob{{Path: "/a", Value: runtime.RawExtension{Raw: []byte(`{}`)}}}
	tests := []struct {
		name    string
		spec    ConfigSetSpec
		wantErr string
	}{
		{"content", ConfigSetSpec{Config: blob}, ""},
		{"sweeper", ConfigSetSpec{DeletePaths: []string{"/"}}, ""},
		{"both", ConfigSetSpec{Config: blob, DeletePaths: []string{"/a"}}, "spec.deletePaths"},
		{"neither", ConfigSetSpec{}, "spec.config"},
		{"bad path", ConfigSetSpec{DeletePaths: []string{"/ok", "/a/[b"}}, "spec.deletePaths[1]"},
	}
	for _, tt := range tests {
		mk := func() *ConfigSet {
			s := tt.spec
			s.Priority = 10
			return &ConfigSet{Spec: s}
		}
		for _, op := range []string{"create", "update"} {
			t.Run(op+"/"+tt.name, func(t *testing.T) {
				cs := mk()
				var joined string
				var list []error
				if op == "create" {
					for _, e := range cs.ValidateCreate(context.Background(), cs) {
						list = append(list, e)
					}
				} else {
					for _, e := range cs.ValidateUpdate(context.Background(), cs, mk()) {
						list = append(list, e)
					}
				}
				for _, e := range list {
					joined += e.Error() + ";"
				}
				if tt.wantErr == "" && joined != "" {
					t.Fatalf("unexpected: %s", joined)
				}
				if tt.wantErr != "" && !strings.Contains(joined, tt.wantErr) {
					t.Fatalf("%q does not mention %q", joined, tt.wantErr)
				}
			})
		}
	}
}
