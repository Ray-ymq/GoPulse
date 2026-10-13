package scenario

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/acceptance/internal/harness"
)

const fakeCandidateDocker = `#!/bin/sh
set -eu

log="${GOPULSE_FAKE_DOCKER_LOG:?}"
mode="${GOPULSE_FAKE_DOCKER_MODE:?}"

if [ "$1" = "image" ] && [ "$2" = "inspect" ]; then
    if [ "$3" != "--platform" ] || [ "$4" != "linux/amd64" ] || [ "$5" != "--format" ]; then
        printf 'inspect platform arguments are not pinned\n' >&2
        exit 2
    fi
    ref="$7"
    count=$(awk -F '|' '$1 == "inspect" { count++ } END { print count + 0 }' "$log")
    printf 'inspect|%s\n' "$ref" >> "$log"
    case "$mode:$count" in
		cached:0|missing:1)
            printf '{"Os":"linux","Architecture":"amd64","RepoDigests":["%s"]}\n' "$ref"
            exit 0
            ;;
		platform-mismatch:0)
            printf '{"Os":"linux","Architecture":"arm64","RepoDigests":["%s"]}\n' "$ref"
            exit 0
            ;;
		digest-mismatch:0)
			printf '{"Os":"linux","Architecture":"amd64","RepoDigests":["registry.example.invalid/gopulse/backend@sha256:%s"]}\n' "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			exit 0
			;;
		no-repodigests:0)
			printf '{"Os":"linux","Architecture":"amd64"}\n'
			exit 0
			;;
		missing:0)
            printf 'Error response from daemon: No such image: %s\n' "$ref" >&2
            exit 1
            ;;
		empty-stderr:0)
			exit 1
			;;
        *)
            printf 'unexpected inspect sequence for %s (%s)\n' "$ref" "$mode" >&2
            exit 2
            ;;
    esac
fi

if [ "$1" = "pull" ]; then
    if [ "$2" != "--platform" ] || [ "$3" != "linux/amd64" ]; then
        printf 'pull platform arguments are not pinned\n' >&2
        exit 2
    fi
    printf 'pull|%s\n' "$4" >> "$log"
    exit 0
fi

printf 'unexpected docker arguments\n' >&2
exit 2
`

func TestConfigureCandidateUsesExactDigestCache(t *testing.T) {
	session, logPath := newCandidateTestSession(t, "cached")
	cleanupCandidateTestSession(t, session)
	ref := testCandidateRef()
	manifestPath := filepath.Join(t.TempDir(), "release-manifest.json")
	manifest := candidateManifest{
		Version:  session.Version,
		Revision: session.Revision,
		Images: map[string]candidateImage{
			"backend": {Ref: "registry.example.invalid/gopulse/backend", Platforms: map[string]string{"linux/amd64": ref[strings.LastIndex(ref, "@")+1:]}},
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := configureCandidate(session, manifestPath); err != nil {
		t.Fatalf("configureCandidate() error = %v", err)
	}
	if got := session.Value("GOPULSE_BACKEND_IMAGE"); got != ref {
		t.Fatalf("configured image = %q, want %q", got, ref)
	}
	if got := readCandidateDockerLog(t, logPath); !reflect.DeepEqual(got, []string{"inspect|" + ref}) {
		t.Fatalf("docker calls = %#v, want one exact-digest inspect", got)
	}
	if len(session.Commands) != 1 || !reflect.DeepEqual(session.Commands[0].Args, []string{"docker", "image", "inspect", "--platform", "linux/amd64", "--format", candidateImageInspectFormat, ref}) {
		t.Fatalf("inspect command = %#v", session.Commands)
	}
}

func TestEnsureCandidateImagePullsMissingDigestAndChecksPlatform(t *testing.T) {
	session, logPath := newCandidateTestSession(t, "missing")
	cleanupCandidateTestSession(t, session)
	ref := testCandidateRef()

	if err := ensureCandidateImage(session, "backend", ref); err != nil {
		t.Fatalf("ensureCandidateImage() error = %v", err)
	}
	got := readCandidateDockerLog(t, logPath)
	want := []string{"inspect|" + ref, "pull|" + ref, "inspect|" + ref}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("docker calls = %#v, want %#v", got, want)
	}
	if len(session.Commands) != 3 || !reflect.DeepEqual(session.Commands[1].Args, []string{"docker", "pull", "--platform", "linux/amd64", ref}) {
		t.Fatalf("pull command did not keep the exact digest reference: %#v", session.Commands)
	}
	if _, err := os.Stat(filepath.Join(session.Root, ".run")); !os.IsNotExist(err) {
		t.Fatalf("cache miss created failure evidence: stat error = %v", err)
	}
}

func TestEnsureCandidateImageRejectsWrongPlatformWithoutPull(t *testing.T) {
	assertCandidateImageInspectionFailure(t, "platform-mismatch", "linux/amd64")
}

func TestEnsureCandidateImageRejectsWrongRepoDigestWithoutPull(t *testing.T) {
	assertCandidateImageInspectionFailure(t, "digest-mismatch", "exact linux/amd64")
}

func TestEnsureCandidateImageRejectsMissingRepoDigestsWithoutPull(t *testing.T) {
	assertCandidateImageInspectionFailure(t, "no-repodigests", "exact linux/amd64")
}

func TestEnsureCandidateImageDoesNotTreatEmptyInspectErrorAsMissing(t *testing.T) {
	assertCandidateImageInspectionFailure(t, "empty-stderr", "exit code 1")
}

func TestCandidateImageInspectionRequiresExactDigestWhenRepoDigestsArePresent(t *testing.T) {
	ref := testCandidateRef()
	if !candidateImageInspectionMatches(ref, candidateImageInspection{OS: "linux", Architecture: "amd64", RepoDigests: []string{ref}}) {
		t.Fatal("matching immutable digest was rejected")
	}
	taggedRef := "registry.example.invalid/gopulse/backend:2.6.5@sha256:" + strings.Repeat("a", 64)
	canonicalRef := "registry.example.invalid/gopulse/backend@sha256:" + strings.Repeat("a", 64)
	if !candidateImageInspectionMatches(taggedRef, candidateImageInspection{OS: "linux", Architecture: "amd64", RepoDigests: []string{canonicalRef}}) {
		t.Fatal("matching digest without the source tag was rejected")
	}
	if candidateImageInspectionMatches(ref, candidateImageInspection{OS: "linux", Architecture: "amd64", RepoDigests: []string{"registry.example.invalid/gopulse/backend@sha256:" + strings.Repeat("b", 64)}}) {
		t.Fatal("different digest was accepted")
	}
	if candidateImageInspectionMatches(ref, candidateImageInspection{OS: "linux", Architecture: "arm64", RepoDigests: []string{ref}}) {
		t.Fatal("arm64 image was accepted")
	}
	if candidateImageInspectionMatches(ref, candidateImageInspection{OS: "linux", Architecture: "amd64"}) {
		t.Fatal("an image without RepoDigests was accepted")
	}
}

func assertCandidateImageInspectionFailure(t *testing.T, mode, message string) {
	t.Helper()
	session, logPath := newCandidateTestSession(t, mode)
	cleanupCandidateTestSession(t, session)
	ref := testCandidateRef()

	err := ensureCandidateImage(session, "backend", ref)
	if err == nil || !strings.Contains(err.Error(), message) {
		t.Fatalf("ensureCandidateImage() error = %v, want %q", err, message)
	}
	if got, want := readCandidateDockerLog(t, logPath), []string{"inspect|" + ref}; !reflect.DeepEqual(got, want) {
		t.Fatalf("docker calls = %#v, want inspect only: %#v", got, want)
	}
}

func cleanupCandidateTestSession(t *testing.T, session *harness.Session) {
	t.Helper()
	t.Cleanup(func() {
		if err := session.Cleanup(); err != nil {
			t.Errorf("session cleanup: %v", err)
		}
	})
}

func newCandidateTestSession(t *testing.T, mode string) (*harness.Session, string) {
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
	if err := os.WriteFile(filepath.Join(fakeBin, "docker"), []byte(fakeCandidateDocker), 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "docker.log")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	session.SetValue("GOPULSE_FAKE_DOCKER_MODE", mode)
	session.SetValue("GOPULSE_FAKE_DOCKER_LOG", logPath)
	return session, logPath
}

func readCandidateDockerLog(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

func testCandidateRef() string {
	return "registry.example.invalid/gopulse/backend@sha256:" + strings.Repeat("a", 64)
}
