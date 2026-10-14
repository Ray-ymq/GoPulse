package packaging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/lifecycle/internal/release"
)

func writeFakeDocker(t *testing.T, body string) (string, string) {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, "docker.log")
	docker := filepath.Join(bin, "docker")
	script := "#!/usr/bin/env bash\nset -eu\nprintf '%s\\n' \"$*\" >> \"$DOCKER_LOG\"\n" + body
	if err := os.WriteFile(docker, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_LOG", log)
	return root, log
}

func writeFakeAcceptance(t *testing.T, root, body string) {
	t.Helper()
	source := filepath.Join(root, "fake-gopulse-acceptance")
	if err := os.WriteFile(source, []byte("#!/usr/bin/env bash\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "acceptance"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	goCommand := filepath.Join(bin, "go")
	if err := os.WriteFile(goCommand, []byte(`#!/usr/bin/env bash
set -eu
output=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    shift
    output="$1"
  fi
  shift
done
cp "$GOPULSE_TEST_ACCEPTANCE_SOURCE" "$output"
chmod 0755 "$output"
`), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPULSE_TEST_ACCEPTANCE_SOURCE", source)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func runtimeManifest() (*release.Manifest, string) {
	indexDigest := "sha256:" + strings.Repeat("a", 64)
	platformDigest := "sha256:" + strings.Repeat("b", 64)
	image := release.Image{
		Ref:       "127.0.0.1:15001/gopulse/lifecycle:2.6.5-candidate-0123456789ab@" + indexDigest,
		Platforms: map[string]string{"linux/amd64": platformDigest},
	}
	m := &release.Manifest{
		Version:   "2.6.5",
		Revision:  strings.Repeat("c", 40),
		Lifecycle: image,
	}
	return m, "127.0.0.1:15001/gopulse/lifecycle:2.6.5-candidate-0123456789ab@" + platformDigest
}

func runtimeRepoDigest() string {
	return "127.0.0.1:15001/gopulse/lifecycle@sha256:" + strings.Repeat("b", 64)
}

func TestNormalizeRepoDigestRemovesTagButPreservesRegistryPort(t *testing.T) {
	ref := "127.0.0.1:15001/gopulse/lifecycle:2.6.5-candidate-0123456789ab@sha256:" + strings.Repeat("a", 64)
	want := "127.0.0.1:15001/gopulse/lifecycle@sha256:" + strings.Repeat("a", 64)
	if got := normalizeRepoDigest(ref); got != want {
		t.Fatalf("unexpected normalized repo digest: got %s, want %s", got, want)
	}
}

func TestInspectLocalThirdPartyImageReusesLockedPlatformImage(t *testing.T) {
	indexDigest := "sha256:" + strings.Repeat("a", 64)
	platformDigest := "sha256:" + strings.Repeat("b", 64)
	image := release.Image{
		Ref:       "redis:7.2.5-alpine@" + indexDigest,
		Platforms: map[string]string{"linux/amd64": platformDigest},
	}
	_, log := writeFakeDocker(t, `
case "$1" in
  image) printf '%s\n' '{"Os":"linux","Architecture":"amd64","RepoDigests":["redis@`+platformDigest+`"],"RootFS":{"Type":"layers","Layers":["sha256:`+strings.Repeat("c", 64)+`"]}}' ;;
  buildx) echo "unexpected registry access" >&2; exit 1 ;;
esac
`)
	if err := inspectLocalThirdPartyImage(image, "linux/amd64"); err != nil {
		t.Fatalf("matching local third-party image should pass: %v", err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "image inspect --platform linux/amd64") || !strings.Contains(got, platformDigest) {
		t.Fatalf("local verification did not inspect the locked platform ref: %s", got)
	}
	if strings.Contains(got, "buildx imagetools") {
		t.Fatalf("local third-party verification contacted a registry: %s", got)
	}
}

func TestEnsureRuntimeImageReusesExactMatchingPlatformDigest(t *testing.T) {
	m, ref := runtimeManifest()
	dockerRoot, log := writeFakeDocker(t, `
case "$1" in
  version) printf '%s\n' '{"Os":"linux","Arch":"amd64"}' ;;
  image) printf '%s\n' '{"Os":"linux","Architecture":"amd64","RepoDigests":["`+runtimeRepoDigest()+`"]}' ;;
  run) printf '%s\n' '{"version":"2.6.5","revision":"`+m.Revision+`","platform":"linux/amd64"}' ;;
  pull) echo "unexpected pull" >&2; exit 1 ;;
esac
`)
	writeFakeAcceptance(t, dockerRoot, "exit 0\n")
	if err := runtimeVerify(&Repo{Root: dockerRoot}, m, filepath.Join(dockerRoot, "release-manifest.json"), "linux/amd64"); err != nil {
		t.Fatalf("matching local image should pass runtime verification: %v", err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "image inspect --platform linux/amd64") || !strings.Contains(got, ref) {
		t.Fatalf("runtime verification must inspect the exact platform ref, got:\n%s", got)
	}
	if strings.Contains(got, "pull ") {
		t.Fatalf("matching local image must not be pulled, got:\n%s", got)
	}
	if !strings.Contains(got, "run --rm --read-only --network none --cap-drop ALL "+ref+" version --json") {
		t.Fatalf("runtime identity probe must still run against the exact ref, got:\n%s", got)
	}
}

func TestEnsureRuntimeImagePullsOnlyWhenImageIsMissing(t *testing.T) {
	_, ref := runtimeManifest()
	matching := `{"Os":"linux","Architecture":"amd64","RepoDigests":["` + runtimeRepoDigest() + `"]}`
	_, log := writeFakeDocker(t, `
case "$1" in
  image)
    if [ "$(grep -c '^image inspect' "$DOCKER_LOG")" -eq 1 ]; then
      echo "Error response from daemon: No such image: candidate" >&2
      exit 1
    fi
    printf '%s\n' '`+matching+`' ;;
  pull) : ;;
esac
`)
	if err := ensureRuntimeImage(ref, "linux/amd64"); err != nil {
		t.Fatalf("an explicitly missing image should be pulled and rechecked: %v", err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if count := strings.Count(got, "image inspect --platform linux/amd64"); count != 2 {
		t.Fatalf("the pulled image must be inspected before and after pull, got %d calls:\n%s", count, got)
	}
	if count := strings.Count(got, "pull "+ref); count != 1 {
		t.Fatalf("the explicitly missing image must use one pull, got %d calls:\n%s", count, got)
	}
}

func TestEnsureRuntimeImageRejectsLocalMetadataMismatchWithoutPull(t *testing.T) {
	cases := []struct {
		name     string
		document string
	}{
		{name: "wrong platform", document: `{"Os":"linux","Architecture":"arm64","RepoDigests":["REF"]}`},
		{name: "wrong digest", document: `{"Os":"linux","Architecture":"amd64","RepoDigests":["127.0.0.1:15001/gopulse/lifecycle@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"]}`},
		{name: "wrong repository", document: `{"Os":"linux","Architecture":"amd64","RepoDigests":["registry.example/gopulse/lifecycle@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"]}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, ref := runtimeManifest()
			document := strings.ReplaceAll(test.document, "REF", ref)
			_, log := writeFakeDocker(t, `
case "$1" in
  image) printf '%s\n' '`+document+`' ;;
  pull) echo "unexpected pull" >&2; exit 1 ;;
esac
`)
			if err := ensureRuntimeImage(ref, "linux/amd64"); err == nil {
				t.Fatal("a local platform or RepoDigest mismatch must fail")
			}
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			got := string(data)
			if strings.Contains(got, "pull ") {
				t.Fatalf("a successfully inspected but mismatched image must not be overwritten, got:\n%s", got)
			}
		})
	}
}

func TestEnsureRuntimeImageRejectsNonMissingInspectErrorWithoutPull(t *testing.T) {
	_, ref := runtimeManifest()
	_, log := writeFakeDocker(t, `
case "$1" in
  image) echo "Error response from daemon: permission denied" >&2; exit 1 ;;
  pull) echo "unexpected pull" >&2; exit 1 ;;
esac
`)
	if err := ensureRuntimeImage(ref, "linux/amd64"); err == nil || !strings.Contains(err.Error(), "cannot inspect local image") {
		t.Fatalf("a non-missing Docker inspect error must fail directly, got %v", err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "pull ") {
		t.Fatalf("a non-missing inspect error must not trigger pull, got:\n%s", data)
	}
}

func TestEnsureRuntimeImageRejectsMismatchedImageAfterPull(t *testing.T) {
	cases := []struct {
		name     string
		document string
	}{
		{name: "wrong platform", document: `{"Os":"linux","Architecture":"arm64","RepoDigests":["REF"]}`},
		{name: "wrong digest", document: `{"Os":"linux","Architecture":"amd64","RepoDigests":["127.0.0.1:15001/gopulse/lifecycle@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"]}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, ref := runtimeManifest()
			document := strings.ReplaceAll(test.document, "REF", ref)
			_, log := writeFakeDocker(t, `
case "$1" in
  image)
    if [ "$(grep -c '^image inspect' "$DOCKER_LOG")" -eq 1 ]; then
      echo "Error response from daemon: No such image: candidate" >&2
      exit 1
    fi
    printf '%s\n' '`+document+`' ;;
  pull) : ;;
esac
`)
			if err := ensureRuntimeImage(ref, "linux/amd64"); err == nil {
				t.Fatal("a pulled image with mismatched metadata must fail")
			}
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			got := string(data)
			if count := strings.Count(got, "image inspect --platform linux/amd64"); count != 2 {
				t.Fatalf("the pulled image must be re-inspected, got %d calls:\n%s", count, got)
			}
			if count := strings.Count(got, "pull "+ref); count != 1 {
				t.Fatalf("the image must be pulled once before recheck, got %d calls:\n%s", count, got)
			}
		})
	}
}

func TestRuntimeVerifyKeepsRuntimeIdentityAssertionAfterLocalReuse(t *testing.T) {
	m, ref := runtimeManifest()
	root, _ := writeFakeDocker(t, `
case "$1" in
  version) printf '%s\n' '{"Os":"linux","Arch":"amd64"}' ;;
  image) printf '%s\n' '{"Os":"linux","Architecture":"amd64","RepoDigests":["`+ref+`"]}' ;;
  run) printf '%s\n' '{"version":"wrong","revision":"`+m.Revision+`","platform":"linux/amd64"}' ;;
esac
`)
	if err := runtimeVerify(&Repo{Root: root}, m, filepath.Join(root, "release-manifest.json"), "linux/amd64"); err == nil || err.Error() != "lifecycle runtime mismatch" {
		t.Fatalf("runtime identity mismatch must remain fatal, got %v", err)
	}
}

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
			writeFakeAcceptance(t, root, test.body)
			// Keep an older executable in the conventional location. The gate
			// must build and run the current source fixture instead of reusing it.
			stale := filepath.Join(root, "acceptance", "bin", "gopulse-acceptance")
			if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(stale, []byte("#!/usr/bin/env bash\necho '[gopulse-acceptance] ERROR: stale runner'\n"), 0o755); err != nil {
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
