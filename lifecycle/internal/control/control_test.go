package control

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

func TestRestoreJournalReusesEpochAndRejectsAnotherSource(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, restoreJournalFile)
	journal := restoreJournal{
		Schema: 1, OperationID: strings.Repeat("a", 64),
		SourceManifest:   "sha256:" + strings.Repeat("b", 64),
		ExpectedOldEpoch: "123e4567-e89b-12d3-a456-426614174000",
		NewEpoch:         "123e4567-e89b-12d3-a456-426614174001",
	}
	if err := saveRestoreJournal(path, journal); err != nil {
		t.Fatal(err)
	}
	loaded, resumed, err := loadRestoreJournal(path, journal.SourceManifest)
	if err != nil || !resumed || loaded != journal {
		t.Fatalf("loaded=%#v resumed=%v err=%v", loaded, resumed, err)
	}
	if _, _, err := loadRestoreJournal(path, "sha256:"+strings.Repeat("c", 64)); err == nil {
		t.Fatal("restore journal was reused for another source manifest")
	}
}

func TestNewEpochIsCanonicalAndUnique(t *testing.T) {
	first, err := newEpoch()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newEpoch()
	if err != nil || first == second || !validEpoch(first) || !validEpoch(second) {
		t.Fatalf("epochs = %q, %q; err=%v", first, second, err)
	}
}

func fixture(t *testing.T) *Controller {
	t.Helper()
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	image := release.Image{Ref: "test/frontend@sha256:" + strings.Repeat("a", 64), Platforms: map[string]string{"linux/amd64": "sha256:" + strings.Repeat("b", 64)}}
	services := map[string]any{"edge": map[string]any{"image": image.Ref, "ports": []any{"port"}, "environment": map[string]any{"AUTH_JWT_SECRET": "${AUTH_JWT_SECRET:?required}", "BUSINESS_STAGE_SERVICE_CREDENTIAL": "${BUSINESS_STAGE_SERVICE_CREDENTIAL:?required}", "VICTORIAMETRICS_PASSWORD": "${VICTORIAMETRICS_PASSWORD:?required}"}}}
	return &Controller{ctx: context.Background(), dir: dir, manifest: &release.Manifest{Version: "1.13.2", Images: map[string]release.Image{"frontend": image}}, manifestHash: "sha256:" + strings.Repeat("c", 64), doc: map[string]any{"services": services, "volumes": map[string]any{"data": map[string]any{}}}, services: services}
}
func TestInitializeDoesNotOverwriteAndPrepareIsRepeatable(t *testing.T) {
	c := fixture(t)
	if e := c.initialize(18080); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(c.dir, "secrets.json"))
	if c.state.Token == c.env["AUTH_JWT_SECRET"] || len(c.env["AUTH_JWT_SECRET"]) != 64 || len(c.env["BUSINESS_STAGE_SERVICE_CREDENTIAL"]) != 64 {
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
	if got := c.redact(c.env["AUTH_JWT_SECRET"] + " " + c.env["BUSINESS_STAGE_SERVICE_CREDENTIAL"] + " " + c.state.Token); got != "[REDACTED] [REDACTED] [REDACTED]" {
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
	image) printf '%s' 'Error response from daemon: No such image: cached' >&2; exit 1;;
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

func TestDistributionDescriptorWithoutOptionalPlatforms(t *testing.T) {
	c := fixture(t)
	socket := filepath.Join(t.TempDir(), "docker.sock")
	listener, e := net.Listen("unix", socket)
	if e != nil {
		t.Fatal(e)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"Descriptor": map[string]string{"digest": digest}, "Platforms": nil})
	})}
	go server.Serve(listener)
	defer server.Close()
	c.endpoint = "unix://" + socket
	if e := c.distribution("registry.example/image@"+digest, true); e != nil {
		t.Fatal("valid child digest rejected", e)
	}
	if e := c.distribution("registry.example/image@sha256:"+strings.Repeat("b", 64), true); e == nil {
		t.Fatal("wrong descriptor accepted")
	}
}

func doctorTestController(t *testing.T, localImages bool) (*Controller, int, string) {
	t.Helper()
	c := fixture(t)
	indexDigest := "sha256:" + strings.Repeat("a", 64)
	platformDigest := "sha256:" + strings.Repeat("b", 64)
	image := release.Image{
		Ref:       "registry.example/gopulse/lifecycle@" + indexDigest,
		Platforms: map[string]string{lifecyclePlatform: platformDigest},
	}
	c.manifest.Lifecycle = image
	c.manifest.Images = map[string]release.Image{}
	c.manifest.ThirdParty = map[string]release.Image{}
	c.localImages = localImages
	c.endpoint = "unix:///does/not/exist"
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return c, port, image.Ref[:strings.LastIndex(image.Ref, "@")] + "@" + platformDigest
}

func doctorDockerScript(inspect string) string {
	return `case "$3/$4" in
 image/inspect) ` + inspect + ` ;;
 info/*) printf '%s' '{"MemTotal":8589934592,"NCPU":2}' ;;
 *) exit 99 ;;
 esac`
}

func TestDoctorLocalImagesValidatesExactPlatformDigestWithoutDistribution(t *testing.T) {
	c, port, _ := doctorTestController(t, true)
	fakeDocker(t, doctorDockerScript(`printf '{"Os":"linux","Architecture":"amd64","RepoDigests":["%s"]}' "$9"`))
	if err := c.doctor(port); err != nil {
		t.Fatalf("local image doctor rejected matching image: %v", err)
	}
}

func TestDoctorLocalImagesRejectsMissingOrInvalidImages(t *testing.T) {
	tests := []struct {
		name    string
		inspect string
		code    int
	}{
		{
			name:    "missing",
			inspect: `printf 'Error response from daemon: No such image: %s\n' "$9" >&2; exit 1`,
			code:    ManifestError,
		},
		{
			name:    "wrong platform",
			inspect: `printf '{"Os":"linux","Architecture":"arm64","RepoDigests":["%s"]}' "$9"`,
			code:    ManifestError,
		},
		{
			name:    "wrong digest",
			inspect: `printf '%s' '{"Os":"linux","Architecture":"amd64","RepoDigests":["registry.example/other@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"]}'`,
			code:    ManifestError,
		},
		{
			name:    "daemon error",
			inspect: `exit 42`,
			code:    Daemon,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, port, _ := doctorTestController(t, true)
			fakeDocker(t, doctorDockerScript(test.inspect))
			err := c.doctor(port)
			failure, ok := err.(*Failure)
			if !ok || failure.Code != test.code || failure.Stage != "local-digest" {
				t.Fatalf("doctor error = %v, want local-digest code %d", err, test.code)
			}
		})
	}
}

func TestDoctorDefaultModeKeepsDistributionValidation(t *testing.T) {
	c, port, _ := doctorTestController(t, false)
	var requests int32
	socket := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		const prefix = "/v1.43/distribution/"
		path := strings.TrimSuffix(strings.TrimPrefix(r.URL.EscapedPath(), prefix), "/json")
		ref, err := url.PathUnescape(path)
		if err != nil {
			http.Error(w, "bad reference", http.StatusBadRequest)
			return
		}
		at := strings.LastIndex(ref, "@")
		if at < 0 {
			http.Error(w, "bad reference", http.StatusBadRequest)
			return
		}
		platforms := []map[string]string(nil)
		if ref == c.manifest.Lifecycle.Ref {
			platforms = []map[string]string{{"OS": "linux", "Architecture": "amd64"}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Descriptor": map[string]string{"digest": ref[at+1:]},
			"Platforms":  platforms,
		})
	})}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Close() }()
	c.endpoint = "unix://" + socket
	fakeDocker(t, doctorDockerScript(`exit 99`))
	if err := c.doctor(port); err != nil {
		t.Fatalf("default distribution doctor rejected valid descriptors: %v", err)
	}
	if got := atomic.LoadInt32(&requests); got != 2 {
		t.Fatalf("distribution requests = %d, want index and platform validation", got)
	}
}
