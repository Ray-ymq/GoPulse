package packaging

import (
	"os"
	"path/filepath"
	"testing"
)

// TestComposeGateRejectsLoggedErrorWithZeroExit mirrors
// test_release_snapshot.test_runtime_gate_rejects_logged_error_with_zero_exit:
// cleanup conditionals can mask errexit, so a reported acceptance error blocks
// the receipt even when the closure exits 0.
func TestComposeGateRejectsLoggedErrorWithZeroExit(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"passing closure", "echo '[gopulse-compose] PASS: complete'\n", false},
		{"logged error with zero exit", "echo '[gopulse-compose] ERROR: fixture'\nexit 0\n", true},
		{"failing closure", "echo '[gopulse-compose] PASS: complete'\nexit 3\n", true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			script := filepath.Join(root, "scripts", "verify-compose.sh")
			if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(script, []byte("#!/usr/bin/env bash\n"+test.body), 0o755); err != nil {
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
