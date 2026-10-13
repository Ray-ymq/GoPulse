package testenv

import "testing"

func TestObserveBrowserSelectionIncludesBusinessStages(t *testing.T) {
	commands, labels := browserCheckCommands(true)
	if len(commands) != 3 || len(labels) != 3 {
		t.Fatalf("observe browser selection = %d commands/%d labels", len(commands), len(labels))
	}
	found := false
	for _, command := range commands {
		for _, argument := range command {
			if argument == "e2e/business-stages.spec.ts" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("observe browser selection omitted business-stages.spec.ts")
	}
	business, _ := browserCheckCommands(false)
	if len(business) != 1 {
		t.Fatalf("business browser selection = %d commands", len(business))
	}
}
