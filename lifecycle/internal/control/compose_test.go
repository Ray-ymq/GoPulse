package control

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/lifecycle/internal/packaging"
	"github.com/Ray-ymq/GoPulse/lifecycle/internal/release"
)

func TestPrepareResolvesBackendAliasesAndPreservesRuntimeLabels(t *testing.T) {
	backend := release.Image{
		Ref:       "test/backend@sha256:" + strings.Repeat("a", 64),
		Platforms: map[string]string{"linux/amd64": "sha256:" + strings.Repeat("b", 64)},
	}
	frontend := release.Image{
		Ref:       "test/frontend@sha256:" + strings.Repeat("c", 64),
		Platforms: map[string]string{"linux/amd64": "sha256:" + strings.Repeat("d", 64)},
	}
	services := map[string]any{
		"edge":         map[string]any{"image": frontend.Ref, "ports": []any{"port"}},
		"backend":      map[string]any{"image": backend.Ref, "labels": map[string]any{"io.gopulse.runtime.process_id": "backend", "io.gopulse.runtime.role": "business-api"}},
		"backend-2":    map[string]any{"image": backend.Ref, "labels": map[string]any{"io.gopulse.runtime.process_id": "backend", "io.gopulse.runtime.role": "business-api"}},
		"platform-api": map[string]any{"image": backend.Ref, "labels": map[string]any{"io.gopulse.runtime.process_id": "backend", "io.gopulse.runtime.role": "platform-api"}},
	}
	c := &Controller{
		ctx:          context.Background(),
		manifest:     &release.Manifest{Images: map[string]release.Image{"backend": backend, "frontend": frontend}},
		manifestHash: "sha256:" + strings.Repeat("e", 64),
		state:        State{Token: "installation-token"},
		services:     services,
		doc:          map[string]any{"services": services},
	}
	if err := c.prepare(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"backend", "backend-2", "platform-api"} {
		service := services[name].(map[string]any)
		if service["image"] != "test/backend@sha256:"+strings.Repeat("b", 64) {
			t.Fatalf("%s image was not resolved to the platform digest: %v", name, service["image"])
		}
		labels := service["labels"].(map[string]any)
		if labels["io.gopulse.runtime.process_id"] != "backend" {
			t.Fatalf("%s runtime process label was lost: %#v", name, labels)
		}
		if labels["io.gopulse.lifecycle.resource"] != name {
			t.Fatalf("%s lifecycle label missing: %#v", name, labels)
		}
	}
	platformLabels := services["platform-api"].(map[string]any)["labels"].(map[string]any)
	if platformLabels["io.gopulse.runtime.role"] != "platform-api" {
		t.Fatalf("platform role label was overwritten: %#v", platformLabels)
	}
}

func TestComposeAliasesCoverDeliveryAliasesAndEdge(t *testing.T) {
	for service, logical := range packaging.ComposeImageAliases() {
		if composeAliases[service] != logical {
			t.Errorf("%s must resolve to %s, got %q", service, logical, composeAliases[service])
		}
	}
	if composeAliases["edge"] != "frontend" {
		t.Errorf("edge must resolve to frontend, got %q", composeAliases["edge"])
	}
}

func TestRuntimeServiceNamesExcludeInitializationJobsAndProfiles(t *testing.T) {
	services := map[string]any{
		"backend":     map[string]any{},
		"edge":        map[string]any{},
		"frontend":    map[string]any{},
		"migrate":     map[string]any{},
		"kafka-init":  map[string]any{},
		"search-init": map[string]any{},
		"admin-role":  map[string]any{"profiles": []any{"operations"}},
	}
	want := []string{"backend", "edge", "frontend"}
	got := runtimeServiceNames(services)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("runtime services = %v, want %v", got, want)
	}
}

func preparedPullFixture(t *testing.T) (*Controller, string) {
	t.Helper()
	c := fixture(t)
	if err := c.prepare(); err != nil {
		t.Fatal(err)
	}
	ref := c.services["edge"].(map[string]any)["image"].(string)
	return c, ref
}

func preparedTaggedPullFixture(t *testing.T) (*Controller, string, string) {
	t.Helper()
	c := fixture(t)
	image := c.manifest.Images["frontend"]
	image.Ref = "registry.example:5000/gopulse/frontend:cached@sha256:" + strings.Repeat("a", 64)
	c.manifest.Images["frontend"] = image
	c.services["edge"].(map[string]any)["image"] = image.Ref
	if err := c.prepare(); err != nil {
		t.Fatal(err)
	}
	ref := c.services["edge"].(map[string]any)["image"].(string)
	canonical := strings.Replace(ref, ":cached@", "@", 1)
	return c, ref, canonical
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func TestPullReusesMatchingLocalImage(t *testing.T) {
	c, ref := preparedPullFixture(t)
	pullMarker := filepath.Join(t.TempDir(), "pull")
	fakeDocker(t, fmt.Sprintf(`case "$3/$4" in
 image/inspect) printf '%%s' '%s' ;;
 pull/*) : > %s; exit 99 ;;
 *) exit 99 ;;
 esac`, `{"Os":"linux","Architecture":"amd64","RepoDigests":["`+ref+`"]}`, shellQuote(pullMarker)))
	if err := c.pull(); err != nil {
		t.Fatalf("matching local image should be reused: %v", err)
	}
	if _, err := os.Stat(pullMarker); !os.IsNotExist(err) {
		t.Fatal("matching local image must not be pulled")
	}
}

func TestPullUsesPullPathOnlyForExplicitlyMissingImage(t *testing.T) {
	c, ref := preparedPullFixture(t)
	inspectMarker := filepath.Join(t.TempDir(), "inspect")
	pullMarker := filepath.Join(t.TempDir(), "pull")
	fakeDocker(t, fmt.Sprintf(`case "$3/$4" in
 image/inspect)
   if [ ! -e %s ]; then : > %s; printf 'Error response from daemon: No such image: %%s\n' '%s' >&2; exit 1; fi
   printf '%%s' '%s' ;;
 pull/*) : > %s ;;
 *) exit 99 ;;
 esac`, shellQuote(inspectMarker), shellQuote(inspectMarker), ref, `{"Os":"linux","Architecture":"amd64","RepoDigests":["`+ref+`"]}`, shellQuote(pullMarker)))
	if err := c.pull(); err != nil {
		t.Fatalf("an explicitly missing image should use the pull path: %v", err)
	}
	if _, err := os.Stat(pullMarker); err != nil {
		t.Fatalf("missing image was not pulled: %v", err)
	}
}

func TestPullRejectsLocalImageValidationFailuresWithoutPulling(t *testing.T) {
	cases := []struct {
		name    string
		inspect string
	}{
		{name: "wrong platform", inspect: `{"Os":"linux","Architecture":"arm64","RepoDigests":["REF"]}`},
		{name: "wrong digest", inspect: `{"Os":"linux","Architecture":"amd64","RepoDigests":["wrong@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"]}`},
		{name: "malformed metadata", inspect: `{not-json`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			c, ref := preparedPullFixture(t)
			pullMarker := filepath.Join(t.TempDir(), "pull")
			inspect := strings.ReplaceAll(test.inspect, "REF", ref)
			fakeDocker(t, fmt.Sprintf(`case "$3/$4" in
 image/inspect) printf '%%s' '%s' ;;
 pull/*) : > %s ;;
 *) exit 99 ;;
 esac`, inspect, shellQuote(pullMarker)))
			if err := c.pull(); err == nil {
				t.Fatal("invalid local image was accepted")
			}
			if _, err := os.Stat(pullMarker); !os.IsNotExist(err) {
				t.Fatal("local validation failure must not fall back to pull")
			}
		})
	}
}

func TestPullRejectsDaemonInspectFailureWithoutPulling(t *testing.T) {
	c, _ := preparedPullFixture(t)
	pullMarker := filepath.Join(t.TempDir(), "pull")
	fakeDocker(t, fmt.Sprintf(`case "$3/$4" in
 image/inspect) exit 1 ;;
 pull/*) : > %s ;;
 *) exit 99 ;;
 esac`, shellQuote(pullMarker)))
	if err := c.pull(); err == nil {
		t.Fatal("daemon inspect failure was treated as a cache miss")
	}
	if _, err := os.Stat(pullMarker); !os.IsNotExist(err) {
		t.Fatal("daemon inspect failure must not fall back to pull")
	}
}

func TestPullRejectsTagOnlyImageReference(t *testing.T) {
	c := fixture(t)
	c.services["edge"].(map[string]any)["image"] = "test/frontend:cached"
	pullMarker := filepath.Join(t.TempDir(), "pull")
	fakeDocker(t, fmt.Sprintf(`case "$3" in
 pull) : > %s ;;
 *) exit 99 ;;
 esac`, shellQuote(pullMarker)))
	if err := c.pull(); err == nil {
		t.Fatal("tag-only image reference was accepted")
	}
	if _, err := os.Stat(pullMarker); !os.IsNotExist(err) {
		t.Fatal("tag-only image reference must not be pulled")
	}
}

func TestPullVerifiesImageAgainAfterPull(t *testing.T) {
	c, ref := preparedPullFixture(t)
	inspectMarker := filepath.Join(t.TempDir(), "inspect")
	pullMarker := filepath.Join(t.TempDir(), "pull")
	fakeDocker(t, fmt.Sprintf(`case "$3/$4" in
 image/inspect)
   if [ ! -e %s ]; then : > %s; printf 'Error response from daemon: No such image: %%s\n' '%s' >&2; exit 1; fi
   printf '%%s' '{"Os":"linux","Architecture":"arm64","RepoDigests":["%s"]}' ;;
 pull/*) : > %s ;;
 *) exit 99 ;;
 esac`, shellQuote(inspectMarker), shellQuote(inspectMarker), ref, ref, shellQuote(pullMarker)))
	if err := c.pull(); err == nil {
		t.Fatal("pull result was not revalidated")
	}
	if _, err := os.Stat(pullMarker); err != nil {
		t.Fatalf("expected the missing image to be pulled: %v", err)
	}
}

func TestPullAcceptsDockerCanonicalRepoDigest(t *testing.T) {
	c, _, canonical := preparedTaggedPullFixture(t)
	fakeDocker(t, fmt.Sprintf(`case "$3/$4" in
 image/inspect) printf '%%s' '{"Os":"linux","Architecture":"amd64","RepoDigests":["%s"]}' ;;
 pull/*) exit 99 ;;
 *) exit 99 ;;
 esac`, canonical))
	if err := c.pull(); err != nil {
		t.Fatalf("canonical Docker repo digest should match the immutable reference: %v", err)
	}
}

func TestPullRejectsCanonicalDigestFromAnotherRepository(t *testing.T) {
	for _, test := range []struct {
		name   string
		actual func(canonical string) string
	}{
		{name: "different repository", actual: func(canonical string) string {
			return strings.Replace(canonical, "/gopulse/frontend@", "/other/frontend@", 1)
		}},
		{name: "registry port dropped", actual: func(canonical string) string {
			return strings.Replace(canonical, "registry.example:5000/", "registry.example/", 1)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _, canonical := preparedTaggedPullFixture(t)
			pullMarker := filepath.Join(t.TempDir(), "pull")
			actual := test.actual(canonical)
			fakeDocker(t, fmt.Sprintf(`case "$3/$4" in
 image/inspect) printf '%%s' '{"Os":"linux","Architecture":"amd64","RepoDigests":["%s"]}' ;;
 pull/*) : > %s ;;
 *) exit 99 ;;
 esac`, actual, shellQuote(pullMarker)))
			if err := c.pull(); err == nil {
				t.Fatal("digest from a different repository was accepted")
			}
			if _, err := os.Stat(pullMarker); !os.IsNotExist(err) {
				t.Fatal("repository mismatch must not fall back to pull")
			}
		})
	}
}
