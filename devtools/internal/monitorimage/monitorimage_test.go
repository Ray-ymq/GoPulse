package monitorimage

import "testing"

func TestReuseRequiresMatchingInputsAndRevision(t *testing.T) {
	for _, test := range []struct {
		name, labels, input, revision string
		want                          bool
	}{
		{"same source", `{"io.gopulse.monitor.input-digest":"source","org.opencontainers.image.revision":"commit"}`, "source", "commit", true},
		{"another build", `{"io.gopulse.monitor.input-digest":"other","org.opencontainers.image.revision":"commit"}`, "source", "commit", false},
		{"another commit", `{"io.gopulse.monitor.input-digest":"source","org.opencontainers.image.revision":"other"}`, "source", "commit", false},
		{"unlabelled image", `{}`, "source", "commit", false},
		{"null labels", `null`, "source", "commit", false},
		{"invalid response", `not json`, "source", "commit", false},
		{"missing inputs", `{}`, "", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := matches(test.labels, test.input, test.revision); got != test.want {
				t.Fatalf("reusable=%v, want %v", got, test.want)
			}
		})
	}
	if Tag != "gopulse/monitor:latest" {
		t.Fatalf("unexpected Monitor image tag: %s", Tag)
	}
}
