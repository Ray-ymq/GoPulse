package control

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComposeFailureCapturesOnlyRedactedBoundedOutput(t *testing.T) {
	c := fixture(t)
	if err := c.initialize(18080); err != nil {
		t.Fatal(err)
	}
	secret := c.env["AUTH_JWT_SECRET"]
	fakeDocker(t, `case "$3" in
compose) printf 'compose failed AUTH_JWT_SECRET=%s\n' "$AUTH_JWT_SECRET" >&2; exit 18 ;;
*) exit 99 ;;
esac`)
	if err := c.phase("infrastructure"); err != nil {
		t.Fatal(err)
	}
	if err := c.compose("up", "-d", "edge"); err == nil {
		t.Fatal("expected Compose failure")
	}
	if c.lastComposeFailure == nil || !strings.Contains(c.lastComposeFailure.Stderr, "compose failed") {
		t.Fatalf("Compose stderr was not captured: %#v", c.lastComposeFailure)
	}
	if strings.Contains(c.lastComposeFailure.Stderr, secret) || !strings.Contains(c.lastComposeFailure.Stderr, "[REDACTED]") {
		t.Fatalf("Compose stderr was not redacted: %q", c.lastComposeFailure.Stderr)
	}
	if len(c.lastComposeFailure.Stderr) > composeDiagnosticLimit {
		t.Fatalf("Compose stderr exceeded diagnostic bound: %d", len(c.lastComposeFailure.Stderr))
	}
}

func TestFailureDiagnosticSavesRedactedStatusAndLogs(t *testing.T) {
	c := fixture(t)
	if err := c.initialize(18080); err != nil {
		t.Fatal(err)
	}
	if err := c.phase("infrastructure"); err != nil {
		t.Fatal(err)
	}
	secret := c.env["AUTH_JWT_SECRET"]
	labels := c.labels("edge")
	labels["com.docker.compose.project"] = c.state.Project
	labels["com.docker.compose.service"] = "edge"
	image := c.services["edge"].(map[string]any)["image"].(string)
	labelJSON, err := json.Marshal(labels)
	if err != nil {
		t.Fatal(err)
	}
	containerJSON := fmt.Sprintf(`[{"Id":"container-id","Config":{"Image":%q,"Labels":%s},"State":{"Status":"exited","ExitCode":18}}]`, image, labelJSON)
	fakeDocker(t, fmt.Sprintf(`case "$3" in
ps) printf 'container-id\n' ;;
inspect) printf '%%s' '%s' ;;
logs) printf 'service reached marker AUTH_JWT_SECRET=%%s password="compose-secret"\n' "$AUTH_JWT_SECRET" ;;
*) exit 99 ;;
esac`, containerJSON))
	c.lastComposeFailure = &composeFailure{Action: "up -d", ExitCode: 18, Stderr: "password=compose-secret " + secret}
	err = c.captureFailureDiagnostic(fail(Failed, "infrastructure", "Compose failed"), "up")
	failure, ok := err.(*Failure)
	if !ok || failure.Diagnostic == "" {
		t.Fatalf("diagnostic path missing: %v", err)
	}
	raw, err := os.ReadFile(failure.Diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	var record lifecycleFailureDiagnostic
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	if got := record.Services["edge"]; got.State != "exited" || got.ExitCode != 18 {
		t.Fatalf("unexpected service status: %#v", record.Services)
	}
	if !strings.Contains(record.Logs["edge"], "service reached marker") {
		t.Fatalf("service logs were not saved: %#v", record.Logs)
	}
	if string(raw) == "" || strings.Contains(string(raw), secret) || strings.Contains(string(raw), "compose-secret") || strings.Contains(string(raw), `"environment"`) {
		t.Fatalf("diagnostic leaked sensitive data: %s", raw)
	}
	info, err := os.Stat(failure.Diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("diagnostic permissions = %o, want 600", info.Mode().Perm())
	}
	if info, err := os.Stat(filepath.Dir(failure.Diagnostic)); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("diagnostic directory permissions = %v %o, want 700", err, info.Mode().Perm())
	}
}
