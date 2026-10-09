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
	ref := "registry.example/gopulse/backend:2.5.4-candidate-0123456789ab"
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
	ref := "127.0.0.1:15001/gopulse/backend:2.5.4-candidate-0123456789ab@sha256:" + strings.Repeat("a", 64)
	if got := releaseTarget(ref, "2.5.4"); got != "127.0.0.1:15001/gopulse/backend:2.5.4" {
		t.Fatalf("unexpected promotion target: %s", got)
	}
}
