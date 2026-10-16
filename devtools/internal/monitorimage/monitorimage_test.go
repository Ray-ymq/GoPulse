package monitorimage

import "testing"

func TestReuseRequiresMatchingInputs(t *testing.T) {
	for _, test := range []struct {
		name, labels, input string
		want                bool
	}{
		{"same source", `{"io.gopulse.monitor.input-digest":"source","org.opencontainers.image.revision":"commit"}`, "source", true},
		{"another build", `{"io.gopulse.monitor.input-digest":"other","org.opencontainers.image.revision":"commit"}`, "source", false},
		{"another commit", `{"io.gopulse.monitor.input-digest":"source","org.opencontainers.image.revision":"other"}`, "source", true},
		{"unlabelled image", `{}`, "source", false},
		{"null labels", `null`, "source", false},
		{"invalid response", `not json`, "source", false},
		{"missing inputs", `{}`, "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := matches(test.labels, test.input); got != test.want {
				t.Fatalf("reusable=%v, want %v", got, test.want)
			}
		})
	}
	if Tag != "gopulse/monitor:latest" {
		t.Fatalf("unexpected Monitor image tag: %s", Tag)
	}
}
