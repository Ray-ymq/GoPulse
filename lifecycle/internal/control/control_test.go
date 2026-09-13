package control

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/lifecycle/internal/release"
)

func TestAtomicPrivateAndLock(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	if e := privateDir(dir); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "state.json")
	if e := atomicFile(path, []byte("first")); e != nil {
		t.Fatal(e)
	}
	if e := atomicFile(path, []byte("second")); e != nil {
		t.Fatal(e)
	}
	b, e := readPrivate(path)
	if e != nil || string(b) != "second" {
		t.Fatal("atomic write", e)
	}
	unlock, e := lock(dir)
	if e != nil {
		t.Fatal(e)
	}
	if release, e := lock(dir); e == nil {
		release()
		t.Fatal("concurrent operation accepted")
	}
	unlock()
	os.Chmod(path, 0644)
	if _, e = readPrivate(path); e == nil {
		t.Fatal("public secret accepted")
	}
	os.Remove(path)
	os.Symlink("outside", path)
	if _, e = readPrivate(path); e == nil {
		t.Fatal("symlink accepted")
	}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".pending-") {
			t.Fatal("temporary file leaked")
		}
	}
}
func fixture(t *testing.T) *Controller {
	t.Helper()
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	image := release.Image{Ref: "test/frontend@sha256:" + strings.Repeat("a", 64), Platforms: map[string]string{"linux/amd64": "sha256:" + strings.Repeat("b", 64)}}
	services := map[string]any{"edge": map[string]any{"image": image.Ref, "ports": []any{"port"}, "environment": map[string]any{"AUTH_JWT_SECRET": "${AUTH_JWT_SECRET:?required}", "VICTORIAMETRICS_PASSWORD": "${VICTORIAMETRICS_PASSWORD:?required}"}}}
	return &Controller{ctx: context.Background(), dir: dir, manifest: &release.Manifest{Version: "1.13.2", Images: map[string]release.Image{"frontend": image}}, manifestHash: "sha256:" + strings.Repeat("c", 64), doc: map[string]any{"services": services, "volumes": map[string]any{"data": map[string]any{}}}, services: services}
}
func TestInitializeDoesNotOverwriteAndPrepareIsRepeatable(t *testing.T) {
	c := fixture(t)
	if e := c.initialize(18080); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(c.dir, "secrets.json"))
	if c.state.Token == c.env["AUTH_JWT_SECRET"] || len(c.env["AUTH_JWT_SECRET"]) != 64 {
		t.Fatal("non-random secret")
	}
	for i := 0; i < 2; i++ {
		if e := c.prepare(); e != nil {
			t.Fatal("prepare must be repeatable", e)
		}
	}
	if e := c.initialize(18081); e == nil {
		t.Fatal("reinit accepted")
	}
	after, _ := os.ReadFile(filepath.Join(c.dir, "secrets.json"))
	if string(after) != string(b) {
		t.Fatal("secret overwritten")
	}
	public, _ := json.Marshal(c.public(nil))
	if strings.Contains(string(public), c.state.Token) {
		t.Fatal("token leaked")
	}
	if got := c.redact(c.env["AUTH_JWT_SECRET"] + " " + c.state.Token); got != "[REDACTED] [REDACTED]" {
		t.Fatal("redaction failed")
	}
	labels := c.labels("edge")
	labels["com.docker.compose.project"] = c.state.Project
	if !c.checkLabels(labels, "edge") {
		t.Fatal("owned rejected")
	}
	labels[prefix+"installation"] = "foreign"
	if c.checkLabels(labels, "edge") {
		t.Fatal("foreign accepted")
	}
}
func TestRejectUnsafeCompose(t *testing.T) {
	c := fixture(t)
	c.services["backend"] = map[string]any{"image": c.manifest.Images["frontend"].Ref, "ports": []any{"8080:8080"}}
	if e := c.initialize(18080); e == nil {
		t.Fatal("unsafe template accepted")
	}
	if _, e := os.Stat(filepath.Join(c.dir, "secrets.json")); !os.IsNotExist(e) {
		t.Fatal("wrote secrets before validation")
	}
}

// The Docker CLI is the public process boundary. These tests inject responses at
// that boundary, never read dependency internals or require a foreign server.
func fakeDocker(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	if e := os.WriteFile(filepath.Join(dir, "docker"), []byte("#!/bin/sh\n"+body), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}
func TestServerRejectsWrongArchitectureBeforeMutation(t *testing.T) {
	c := fixture(t)
	fakeDocker(t, `case "$3" in
 version) printf '%s' '{"Os":"linux","Arch":"arm64","Version":"29.0.0"}';;
 *) exit 99;;
 esac`)
	e := c.server()
	failure, ok := e.(*Failure)
	if !ok || failure.Code != Platform {
		t.Fatalf("expected wrong-platform exit, got %v", e)
	}
	fakeDocker(t, `case "$3" in
 version) printf '%s' '{"Os":"linux","Arch":"amd64","Version":"29.0.0"}';;
 compose) printf '%s' '{"version":"v2.24.0"}';;
 *) exit 99;;
 esac`)
	if e := c.server(); e != nil {
		t.Fatal(e)
	}
}
func TestStartupFailureRetainsNonReadyPhase(t *testing.T) {
	c := fixture(t)
	if e := c.initialize(18080); e != nil {
		t.Fatal(e)
	}
	fakeDocker(t, `case "$3" in
 ps|network|volume) exit 0;;
 pull) exit 1;;
 *) exit 99;;
 esac`)
	e := c.up()
	failure, ok := e.(*Failure)
	if !ok || failure.Code != ManifestError || failure.Stage != "pull" {
		t.Fatalf("unexpected startup failure: %v", e)
	}
	b, _ := os.ReadFile(filepath.Join(c.dir, "state.json"))
	var state State
	json.Unmarshal(b, &state)
	if state.Phase != "pull" {
		t.Fatal("failed startup marked ready")
	}
	if _, e := readPrivate(filepath.Join(c.dir, "secrets.json")); e != nil {
		t.Fatal("failure deleted installation secrets")
	}
}
