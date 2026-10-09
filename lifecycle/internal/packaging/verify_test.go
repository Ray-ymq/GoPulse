package packaging

import (
	"os"
	"path/filepath"
	"testing"
)

// TestComposeGateRejectsLoggedErrorWithZeroExit protects the native acceptance
// gate from a reported error hidden by a zero process exit.
// cleanup conditionals can mask errexit, so a reported acceptance error blocks
// the receipt even when the closure exits 0.
func TestComposeGateRejectsLoggedErrorWithZeroExit(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"passing closure", "echo '[gopulse-acceptance] PASS: complete'\n", false},
		{"logged error with zero exit", "echo '[gopulse-acceptance] ERROR: fixture'\nexit 0\n", true},
		{"failing closure", "echo '[gopulse-acceptance] PASS: complete'\nexit 3\n", true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			command := filepath.Join(root, "acceptance", "bin", "gopulse-acceptance")
			if err := os.MkdirAll(filepath.Dir(command), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(command, []byte("#!/usr/bin/env bash\n"+test.body), 0o755); err != nil {
				t.Fatal(err)
			}
			err := runComposeGate(&Repo{Root: root}, filepath.Join(root, "release-manifest.json"))
			if test.wantErr && err == nil {
				t.Error("the Compose gate must fail")
			}
			if !test.wantErr && err != nil {
				t.Errorf("the Compose gate must pass, got %v", err)
			}
		})
	}
}
