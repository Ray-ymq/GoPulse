package scenario

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/acceptance/internal/harness"
)

const fakeFaultControlDocker = `#!/bin/sh
set -eu

log="${GOPULSE_FAULT_DOCKER_LOG:?}"
mode="${GOPULSE_FAULT_DOCKER_MODE:?}"
printf '%s\n' "$*" >> "$log"

case "$1" in
network)
    case "$2" in
    ls) printf 'test-network\n' ;;
    disconnect|connect) ;;
    esac
    ;;
compose)
    printf 'upstream-container\n'
    ;;
create)
    if [ "$mode" = "create-fail" ]; then
        exit 19
    fi
    ;;
start)
    if [ "$mode" = "start-fail" ]; then
        exit 17
    fi
    ;;
inspect)
    printf '{"Status":"created","Running":false} {"19125/tcp":null}\n'
    ;;
logs)
    printf 'fixture output\n'
    ;;
rm)
    ;;
*)
    printf 'unexpected docker command: %s\n' "$*" >&2
    exit 23
    ;;
esac
`

const fakeFaultControlGo = `#!/bin/sh
set -eu

output=""
previous=""
for argument in "$@"; do
    if [ "$previous" = "-o" ]; then
        output="$argument"
    fi
    previous="$argument"
done
if [ -n "$output" ]; then
    : > "$output"
fi
`

func TestDependencyControlUpstreamUsesURLOnlyForElasticsearch(t *testing.T) {
	tests := []struct {
		service string
		port    string
		want    string
	}{
		{service: controlMySQL, port: "3306", want: "mysql-upstream:3306"},
		{service: controlRabbitMQ, port: "5672", want: "rabbitmq-upstream:5672"},
		{service: controlElasticsearch, port: "9200", want: "http://elasticsearch-upstream:9200"},
	}
	for _, test := range tests {
		t.Run(test.service, func(t *testing.T) {
			if got := dependencyControlUpstream(test.service, test.port); got != test.want {
				t.Fatalf("dependencyControlUpstream(%q, %q) = %q, want %q", test.service, test.port, got, test.want)
			}
		})
	}
}

func TestStartDependencyControlFailurePreservesContainerDiagnostics(t *testing.T) {
	session, logPath := newFaultControlTestSession(t, "start-fail")
	defer cleanupFaultControlTestSession(t, session)

	_, err := (&formalV1Runner{session: session}).startMySQLControl("commit-result")
	if err == nil || err.Error() != "start mysql protocol control failed with exit code 17" {
		t.Fatalf("startMySQLControl() error = %v", err)
	}
	if got := session.Receipt().Error; got != err.Error() {
		t.Fatalf("receipt error = %q, want original error %q", got, err)
	}

	files := faultControlCommandFilesUnder(t, session.Root)
	var inspect, logs string
	for _, content := range files {
		if strings.Contains(content, "docker inspect") {
			inspect = content
		}
		if strings.Contains(content, "docker logs") {
			logs = content
		}
	}
	if !strings.Contains(inspect, "{{json .State}}") || !strings.Contains(inspect, "{{json .NetworkSettings.Ports}}") {
		t.Fatalf("inspect command did not keep the bounded state/ports format: %s", inspect)
	}
	if strings.Contains(inspect, "Env") {
		t.Fatalf("inspect diagnostic included Env: %s", inspect)
	}
	if !strings.Contains(inspect, "Status") || !strings.Contains(inspect, "19125/tcp") {
		t.Fatalf("inspect output was not retained: %s", inspect)
	}
	if !strings.Contains(logs, "docker logs --tail 100") || !strings.Contains(logs, "fixture output") {
		t.Fatalf("container logs were not retained: %s", logs)
	}
	if got := readFaultControlLog(t, logPath); !strings.Contains(got, "rm -f") {
		t.Fatalf("created container was not owned-cleaned: %s", got)
	}
	got := readFaultControlLog(t, logPath)
	controlNetwork := session.Project + "-fault-control-mysql-" + session.Token[:8]
	if !faultControlLogHasCommand(got, "network create --label io.gopulse.acceptance.project="+session.Project+" --label io.gopulse.acceptance.owner=v1-fault-control "+controlNetwork) {
		t.Fatalf("owned external control network was not created: %s", got)
	}
	if !faultControlLogHasCommand(got, "network connect --alias mysql test-network") {
		t.Fatalf("fault proxy was not attached to the private business network: %s", got)
	}
	if !strings.Contains(got, "--network "+controlNetwork+" --publish 127.0.0.1::19125") {
		t.Fatalf("fault proxy was not created on the external control network: %s", got)
	}
	if !faultControlLogHasCommand(got, "network rm "+controlNetwork) {
		t.Fatalf("owned external control network was not removed: %s", got)
	}
}

func TestStartDependencyControlFailureSkipsDiagnosticsForUncreatedContainer(t *testing.T) {
	session, logPath := newFaultControlTestSession(t, "create-fail")
	defer cleanupFaultControlTestSession(t, session)

	_, err := (&formalV1Runner{session: session}).startMySQLControl("commit-result")
	if err == nil || err.Error() != "create mysql protocol control failed with exit code 19" {
		t.Fatalf("startMySQLControl() error = %v", err)
	}
	got := readFaultControlLog(t, logPath)
	if faultControlLogHasCommand(got, "inspect") || faultControlLogHasCommand(got, "logs") || strings.Contains(got, "rm -f") {
		t.Fatalf("diagnostic or removal command ran for an uncreated container: %s", got)
	}
	if !strings.Contains(got, "network connect --alias mysql") {
		t.Fatalf("upstream network ownership was not restored: %s", got)
	}
}

func newFaultControlTestSession(t *testing.T, mode string) (*harness.Session, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte("2.6.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env.example"), []byte("TEST_VALUE=test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, output)
	}
	commit := exec.Command("git", "-C", root, "-c", "user.name=Acceptance Test", "-c", "user.email=acceptance@example.invalid", "commit", "--quiet", "--allow-empty", "-m", "test")
	if output, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v (%s)", err, output)
	}
	session, err := harness.New(root, false)
	if err != nil {
		t.Fatal(err)
	}
	fakeBin := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeBin, "docker"), []byte(fakeFaultControlDocker), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "go"), []byte(fakeFaultControlGo), 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "docker.log")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	session.SetValue("GOPULSE_FAULT_DOCKER_MODE", mode)
	session.SetValue("GOPULSE_FAULT_DOCKER_LOG", logPath)
	return session, logPath
}

func cleanupFaultControlTestSession(t *testing.T, session *harness.Session) {
	t.Helper()
	if err := session.Cleanup(); err != nil {
		t.Errorf("session cleanup: %v", err)
	}
}

func faultControlCommandFilesUnder(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	base := filepath.Join(root, ".run")
	err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "command-") || !strings.HasSuffix(entry.Name(), ".txt") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		files = append(files, string(content))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func faultControlLogHasCommand(log, command string) bool {
	for _, line := range strings.Split(log, "\n") {
		if line == command || strings.HasPrefix(line, command+" ") {
			return true
		}
	}
	return false
}

func readFaultControlLog(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
