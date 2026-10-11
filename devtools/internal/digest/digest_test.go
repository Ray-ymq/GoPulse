package digest

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDigestIsDeterministicAndChangeSensitive(t *testing.T) {
	root := t.TempDir()
	write(t, root, "backend/main.go", "package main\n")
	write(t, root, "backend/nested/file.go", "package nested\n")

	first, err := Paths(root, []string{filepath.Join(root, "backend")})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Paths(root, []string{"backend"})
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("the same tree must digest identically: %s != %s", first, second)
	}

	// A path change is a content change: build inputs are addressed by path too.
	if err := os.Rename(filepath.Join(root, "backend", "nested", "file.go"), filepath.Join(root, "backend", "nested", "renamed.go")); err != nil {
		t.Fatal(err)
	}
	renamed, err := Paths(root, []string{"backend"})
	if err != nil {
		t.Fatal(err)
	}
	if renamed == first {
		t.Error("renaming a file must change the digest")
	}

	write(t, root, "backend/main.go", "package main // changed\n")
	changed, err := Paths(root, []string{"backend"})
	if err != nil {
		t.Fatal(err)
	}
	if changed == renamed {
		t.Error("changing file content must change the digest")
	}
}

func TestDigestIgnoresGitDirectory(t *testing.T) {
	root := t.TempDir()
	write(t, root, "backend/main.go", "package main\n")
	before, err := Paths(root, []string{"backend"})
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "backend/.git/objects/aa/bb", "object")
	after, err := Paths(root, []string{"backend"})
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Error("a .git directory must not contribute to a source digest")
	}
}

func TestSourceDigestCoversLocalReplaceModulesAndScope(t *testing.T) {
	root := t.TempDir()
	write(t, root, "backend/go.mod", "module example\n\nreplace github.com/Ray-ymq/GoPulse/componentmetrics => ../componentmetrics\n")
	write(t, root, "componentmetrics/metrics.go", "package componentmetrics\n")
	write(t, root, "router/main.go", "package main\n")
	write(t, root, "marshaller/main.go", "package main\n")

	development, err := Source(root, false)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := Source(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if development == observation {
		t.Fatal("the observation scope must digest Router and Marshaller as well")
	}

	// The replaced helper's regression: a locally replaced module is a build input.
	write(t, root, "componentmetrics/metrics.go", "package componentmetrics // changed\n")
	changedDevelopment, err := Source(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if changedDevelopment == development {
		t.Error("a local replace module must contribute to the source digest")
	}
	changedObservation, err := Source(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if changedObservation == changedDevelopment {
		t.Error("the observation digest must keep its extra inputs")
	}
}

func TestMonitorInputDigestCoversPackagingInputs(t *testing.T) {
	root := t.TempDir()
	write(t, root, "VERSION", "2.5.3\n")
	write(t, root, "monitor/main.go", "package main\n")
	write(t, root, "componentmetrics/metrics.go", "package componentmetrics\n")
	write(t, root, "exporters/redis/main.go", "package main\n")
	write(t, root, "deploy/docker/observability.Dockerfile", "FROM scratch\n")
	write(t, root, "deploy/plugins/redis-exporter.json", "{}\n")

	before, err := MonitorInput(root)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"VERSION":                                "2.5.4\n",
		"monitor/main.go":                        "package main // changed\n",
		"deploy/docker/observability.Dockerfile": "FROM scratch\n# changed\n",
		"deploy/plugins/redis-exporter.json":     "{\"changed\": true}\n",
	} {
		write(t, root, name, content)
		after, err := MonitorInput(root)
		if err != nil {
			t.Fatal(err)
		}
		if after == before {
			t.Errorf("%s must contribute to the Monitor input digest", name)
		}
		write(t, root, name, content)
		before = after
	}
}

func TestE2ESourceDigestCoversFrontendAndAdminScope(t *testing.T) {
	root := t.TempDir()
	write(t, root, "frontend/src/main.ts", "export {}\n")
	write(t, root, "frontend/e2e/business.spec.ts", "test()\n")
	write(t, root, "frontend/package.json", "{}\n")
	write(t, root, "admin-frontend/src/main.ts", "export {}\n")

	business, err := E2ESource(root, false)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := E2ESource(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if business == observation {
		t.Fatal("the observation scope must digest the admin frontend as well")
	}
	write(t, root, "admin-frontend/src/main.ts", "export {} // changed\n")
	changedObservation, err := E2ESource(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if changedObservation == observation {
		t.Error("admin frontend sources must contribute to the observation digest")
	}
	unchangedBusiness, err := E2ESource(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if unchangedBusiness != business {
		t.Error("an admin frontend change must not invalidate the business scope")
	}
}
