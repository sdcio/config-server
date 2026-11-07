package handlers

import (
	"testing"

	"github.com/sdcio/config-server/apis/config"
	"k8s.io/utils/ptr"
)

func TestBuildDryRunIntentsSweeper(t *testing.T) {
	c := &config.Config{Spec: config.ConfigSpec{
		Priority:    5,
		Revertive:   ptr.To(false),
		DeletePaths: []string{"/interfaces"},
	}}
	intents, err := buildDryRunIntents(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(intents) != 1 || len(intents[0].Deletes) != 1 || len(intents[0].Update) != 0 {
		t.Fatalf("unexpected intents: %v", intents)
	}
	if intents[0].Priority != 5 || intents[0].NonRevertive {
		t.Errorf("priority/revertive wrong: %v", intents[0])
	}
}
