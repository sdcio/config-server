package config

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

func TestBuildTransactionIntent(t *testing.T) {
	cfg := &Config{
		Spec: ConfigSpec{
			Priority:  7,
			Revertive: ptr.To(false),
			Config:    []ConfigBlob{{Path: "/interfaces", Value: runtime.RawExtension{Raw: []byte(`{"a":1}`)}}},
		},
		Status: ConfigStatus{AppliedConfig: &ConfigSpec{
			Config: []ConfigBlob{
				{Path: "/system", Value: runtime.RawExtension{Raw: []byte(`{"b":2}`)}},
				{Path: "/acl", Value: runtime.RawExtension{Raw: []byte(`{"c":3}`)}},
			},
		}},
	}

	tests := []struct {
		name         string
		useSpec      bool
		wantUpdates  int
		wantPath     string
		wantPrevious bool
	}{
		{"spec", true, 1, "interfaces", false},
		{"applied", false, 2, "system", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildTransactionIntent(cfg, tt.useSpec)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Update) != tt.wantUpdates {
				t.Fatalf("updates = %d, want %d", len(got.Update), tt.wantUpdates)
			}
			if name := got.Update[0].Path.Elem[0].Name; name != tt.wantPath {
				t.Errorf("first path = %q, want %q", name, tt.wantPath)
			}
			if got.Priority != 7 {
				t.Errorf("priority = %d", got.Priority)
			}
			if !got.NonRevertive {
				t.Errorf("NonRevertive = false, want true")
			}
			if got.PreviouslyApplied != tt.wantPrevious {
				t.Errorf("PreviouslyApplied = %v", got.PreviouslyApplied)
			}
			if got.Intent != GetGVKNSN(cfg) {
				t.Errorf("intent = %q", got.Intent)
			}
		})
	}
}
