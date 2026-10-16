package packaging

import (
	"os"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/lifecycle/internal/release"
)

func TestPromotionOnlyConfirmsTheRecordedLatestDigest(t *testing.T) {
	index := `{"schemaVersion":2,"manifests":[]}`
	ref := "127.0.0.1:15001/gopulse/backend:latest@" + Sum([]byte(index))
	image := release.Image{Ref: ref}
	for _, test := range []struct {
		name, response string
		wantError      bool
	}{
		{"unchanged latest", index, false},
		{"latest replaced by another build", `{}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, log := writeFakeDocker(t, `
case "$3" in
  inspect) printf '%s' '`+test.response+`' ;;
  create) ;;
  *) exit 1 ;;
esac
`)
			if err := promoteImage(image); (err != nil) != test.wantError {
				t.Fatalf("promotion error=%v, want error=%v", err, test.wantError)
			}
			raw, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			commands := string(raw)
			if !strings.Contains(commands, "buildx imagetools inspect --raw 127.0.0.1:15001/gopulse/backend:latest") || strings.Contains(commands, "buildx imagetools create") {
				t.Fatalf("unexpected promotion commands: %s", commands)
			}
		})
	}
}
