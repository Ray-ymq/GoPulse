package scenario

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/acceptance/internal/harness"
)

const fakeLifecycleRunnerDocker = `#!/bin/sh
printf '%s|%s\n' "$GOPULSE_BUNDLE_DIR" "$GOPULSE_INSTALL_DIR" >> "$GOPULSE_TEST_LOG"
case "$GOPULSE_BUNDLE_DIR" in
  *bad-bundle) exit 10 ;;
esac
exit 0
`

func TestLocalImageValidationFlagsPinLocalDigestChecks(t *testing.T) {
	got := localImageValidationFlags("18080")
	want := []string{"--local-images", "--port", "18080"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("local image validation flags = %#v, want %#v", got, want)
	}
}

func TestDirectArgsLowDiskUsesToolIdentity(t *testing.T) {
	got := directArgs(1000, 1001, "989", "/bundle", "/install", true)
	want := []string{
		"run", "--rm", "--network", "host", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--user", "1000:1001", "--group-add", "989", "--tmpfs", "/tmp",
		"-v", "/var/run/docker.sock:/var/run/docker.sock", "-v", "/bundle:/bundle:ro",
		"--tmpfs", "/install:rw,size=1048576,mode=0700,uid=1000,gid=1001",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("direct args = %#v, want %#v", got, want)
	}
}

func TestLifecycleRunnerBadBundleAndInterleavedPaths(t *testing.T) {
	fakeBin := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeBin, "docker"), []byte(fakeLifecycleRunnerDocker), 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "runner-paths.log")
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GOPULSE_TEST_LOG", logPath)

	session := newLifecycleTestSession(t)
	initialBundle := session.Value("GOPULSE_BUNDLE_DIR")
	initialInstall := session.Value("GOPULSE_INSTALL_DIR")
	sourceBundle := filepath.Join(t.TempDir(), "source-bundle")
	sourceInstall := filepath.Join(t.TempDir(), "source-install")
	badBundle := filepath.Join(t.TempDir(), "bad-bundle")
	targetBundle := filepath.Join(t.TempDir(), "target-bundle")
	targetInstall := filepath.Join(t.TempDir(), "target-install")
	source, err := newLifecycleRunner(session, sourceBundle, "clean", sourceInstall)
	if err != nil {
		t.Fatal(err)
	}
	target, err := newLifecycleRunner(session, targetBundle, "clean", targetInstall)
	if err != nil {
		t.Fatal(err)
	}

	bad := *source
	bad.bundle = badBundle
	if err := bad.expect("doctor", 10); err != nil {
		t.Fatalf("bad bundle did not return the expected code 10: %v", err)
	}
	if got := session.Value("GOPULSE_BUNDLE_DIR"); got == badBundle {
		t.Fatalf("bad bundle leaked into the shared session: %q", got)
	}
	if got := session.Value("GOPULSE_INSTALL_DIR"); got == bad.install {
		t.Fatalf("bad install path leaked into the shared session: %q", got)
	}
	if err := source.expect("doctor", 0); err != nil {
		t.Fatalf("source runner was polluted after bad bundle: %v", err)
	}
	if err := target.expect("doctor", 0); err != nil {
		t.Fatalf("target runner failed after source runner: %v", err)
	}
	if err := source.expect("doctor", 0); err != nil {
		t.Fatalf("source runner failed after target runner: %v", err)
	}
	if source.bundle != sourceBundle || source.install != sourceInstall {
		t.Fatalf("source runner path changed: bundle=%q install=%q", source.bundle, source.install)
	}
	if target.bundle != targetBundle || target.install != targetInstall {
		t.Fatalf("target runner path changed: bundle=%q install=%q", target.bundle, target.install)
	}
	if got := session.Value("GOPULSE_BUNDLE_DIR"); got != initialBundle {
		t.Fatalf("shared bundle path changed after interleaved commands: %q", got)
	}
	if got := session.Value("GOPULSE_INSTALL_DIR"); got != initialInstall {
		t.Fatalf("shared install path changed after interleaved commands: %q", got)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(raw)), "\n")
	want := []string{
		badBundle + "|" + sourceInstall,
		sourceBundle + "|" + sourceInstall,
		targetBundle + "|" + targetInstall,
		sourceBundle + "|" + sourceInstall,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("runner paths = %#v, want %#v", got, want)
	}
}

func newLifecycleTestSession(t *testing.T) *harness.Session {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte("2.6.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env.example"), []byte("FOO=bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, output)
	}
	command := exec.Command("git", "-C", root, "add", "VERSION", ".env.example")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v (%s)", err, output)
	}
	command = exec.Command("git", "-C", root, "-c", "user.name=Acceptance Test", "-c", "user.email=acceptance@example.invalid", "commit", "--quiet", "-m", "test")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v (%s)", err, output)
	}
	session, err := harness.New(root, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := session.Cleanup(); err != nil {
			t.Errorf("cleanup test session: %v", err)
		}
	})
	return session
}
