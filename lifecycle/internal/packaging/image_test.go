package packaging

import (
	"strings"
	"testing"
)

// TestImageRecordBindsIndexAndPlatformDigests protects the immutable identity
// of a candidate tag: the recorded index digest is the digest of the bytes the
// registry returned, and unsupported or repeated platforms are rejected.
func TestImageRecordBindsIndexAndPlatformDigests(t *testing.T) {
	platform := func(os, arch, digest string) string {
		return `{"digest":"` + digest + `","platform":{"os":"` + os + `","architecture":"` + arch + `"}}`
	}
	raw := `{"schemaVersion":2,"manifests":[` +
		platform("linux", "amd64", "sha256:"+strings.Repeat("a", 64)) + `,` +
		platform("linux", "arm64", "sha256:"+strings.Repeat("b", 64)) + `,` +
		platform("unknown", "unknown", "sha256:"+strings.Repeat("c", 64)) + `]}`
	ref := "registry.example/gopulse/backend:latest"
	record, err := imageRecord(ref, []byte(raw))
	if err != nil {
		t.Fatalf("a valid index must be recorded: %v", err)
	}
	if record.Ref != ref+"@"+Sum([]byte(raw)) {
		t.Fatalf("the index digest must come from the returned bytes: %s", record.Ref)
	}
	if len(record.Platforms) != 2 || record.Platforms["linux/amd64"] != "sha256:"+strings.Repeat("a", 64) {
		t.Fatalf("supported platforms must be recorded: %+v", record.Platforms)
	}
	duplicate := `{"manifests":[` +
		platform("linux", "amd64", "sha256:"+strings.Repeat("a", 64)) + `,` +
		platform("linux", "amd64", "sha256:"+strings.Repeat("b", 64)) + `]}`
	if _, err := imageRecord(ref, []byte(duplicate)); err == nil {
		t.Error("a duplicated platform digest must be rejected")
	}
	foreign := `{"manifests":[` + platform("linux", "arm64", "sha256:"+strings.Repeat("b", 64)) + `]}`
	if _, err := imageRecord(ref, []byte(foreign)); err == nil {
		t.Error("an index without a linux/amd64 manifest must be rejected")
	}
}

// TestReleaseTargetKeepsTheRepositoryOfARegistryWithAPort reproduces an
// observed defect: splitting on the first colon promoted a repository named
// after the registry host.
func TestReleaseTargetKeepsTheRepositoryOfARegistryWithAPort(t *testing.T) {
	for _, ref := range []string{
		"127.0.0.1:15001/gopulse/backend:latest",
		"127.0.0.1:15001/gopulse/backend",
	} {
		if got := releaseTarget(ref + "@sha256:" + strings.Repeat("a", 64)); got != "127.0.0.1:15001/gopulse/backend:latest" {
			t.Fatalf("unexpected promotion target: %s", got)
		}
	}
}

func TestAllProductTargetsUseLatest(t *testing.T) {
	for _, name := range append(append([]string{}, Products...), "lifecycle") {
		if got, want := releaseTarget("gopulse/"+name), "gopulse/"+name+":latest"; got != want {
			t.Fatalf("image target=%s, want %s", got, want)
		}
	}
}
