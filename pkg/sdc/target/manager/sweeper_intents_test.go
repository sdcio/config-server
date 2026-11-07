package targetmanager

import (
	"testing"

	"github.com/sdcio/config-server/apis/config"
)

func TestBuildRecoveryIntentsUsesAppliedDeletePaths(t *testing.T) {
	cfg := &config.Config{
		Spec: config.ConfigSpec{Priority: 5, DeletePaths: []string{"/new"}},
		Status: config.ConfigStatus{AppliedConfig: &config.ConfigSpec{
			DeletePaths: []string{"/applied"},
		}},
	}
	intents, err := buildRecoveryIntents([]*config.Config{cfg})
	if err != nil {
		t.Fatal(err)
	}
	got := intents[0]
	if len(got.Deletes) != 1 || got.Deletes[0].Elem[0].Name != "applied" {
		t.Fatalf("deletes = %v", got.Deletes)
	}
	if len(got.Update) != 0 || !got.PreviouslyApplied {
		t.Errorf("update=%d previouslyApplied=%v", len(got.Update), got.PreviouslyApplied)
	}
}

func TestBuildDeleteIntentCarriesNoDeletes(t *testing.T) {
	cfg := &config.Config{Spec: config.ConfigSpec{Priority: 5, DeletePaths: []string{"/interfaces"}}}
	got := buildDeleteIntent(cfg)
	if !got.Delete || len(got.Deletes) != 0 || len(got.Update) != 0 || got.Priority != 0 {
		t.Fatalf("unexpected delete intent: %v", got)
	}
}
