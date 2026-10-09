package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/monitor/internal/plugin"
)

const (
	fixturePath = "testdata/plugin-package-fixture"
	// Reference digests were recorded from the retired shell implementation with
	// this exact fixture; published official packages depend on them staying equal.
	referenceV1Redis = "f8a831436d93f8e14a486298cb777537bba6bf51a1862ad1cd93e5ad1540f5d6"
	referenceV2Redis = "cf08b9c99e1bc89fab7be1b3715ef6f3bc8ecd138e41f71682349748ec04f0d2"
	referenceV2MySQL = "6cb6f2aa6a625d45fff992b6c06f08b4e58233c9a50fde6d401fc0614e053ea3"
)

func TestLegacyArchiveMatchesReference(t *testing.T) {
	output := filepath.Join(t.TempDir(), "legacy.tar.gz")
	path, err := run([]string{"--contract-version", "1", "--version", "1.10.6", "--arch", "amd64",
		"--binary", fixturePath, "--output", output})
	if err != nil {
		t.Fatal(err)
	}
	if path != output {
		t.Fatalf("output path = %q, want %q", path, output)
	}
	if digest := archiveDigest(t, path); digest != referenceV1Redis {
		t.Fatalf("legacy archive digest = %s, want %s", digest, referenceV1Redis)
	}
	reference, err := os.ReadFile("testdata/legacy-v1-reference.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	built, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(built) != string(reference) {
		t.Fatal("legacy archive is not byte identical to the committed reference")
	}
	members := archiveMembers(t, path)
	if strings.Join(members, ",") != "plugin.json,bin/gopulse-redis-exporter" {
		t.Fatalf("legacy members = %v", members)
	}
}

func TestCurrentArchivesMatchReference(t *testing.T) {
	cases := []struct{ source, digest, entrypoint string }{
		{"redis", referenceV2Redis, "bin/gopulse-redis-exporter"},
		{"mysql", referenceV2MySQL, "bin/gopulse-mysql-exporter"},
	}
	for _, test := range cases {
		t.Run(test.source, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "current.tar.gz")
			if _, err := run([]string{"--source", test.source, "--contract-version", "2", "--version", "2.5.2",
				"--arch", "amd64", "--binary", fixturePath, "--output", output}); err != nil {
				t.Fatal(err)
			}
			if digest := archiveDigest(t, output); digest != test.digest {
				t.Fatalf("archive digest = %s, want %s", digest, test.digest)
			}
			members := archiveMembers(t, output)
			if strings.Join(members, ",") != "plugin.json,config.schema.json,"+test.entrypoint {
				t.Fatalf("members = %v", members)
			}
			manifest, schema := readManifestAndSchema(t, output)
			parsed, err := plugin.ParseManifestV2(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := plugin.ValidateConfigSchema(schema, parsed); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDefaultsComeFromRepositoryRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte("2.5.2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := run([]string{"--arch", "amd64", "--binary", fixturePath, "--repo-root", root})
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(root, ".run", "packages", "gopulse-redis-exporter-2.5.2-linux-amd64.tar.gz")
	if path != expected {
		t.Fatalf("default output = %q, want %q", path, expected)
	}
}

func TestInvalidInvocationsFailAsUsage(t *testing.T) {
	nonExecutable := filepath.Join(t.TempDir(), "plain.txt")
	if err := os.WriteFile(nonExecutable, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name      string
		arguments []string
	}{
		{"invalid version", []string{"--version", "2.5", "--arch", "amd64", "--binary", fixturePath}},
		{"unknown source", []string{"--source", "postgres", "--version", "2.5.2", "--arch", "amd64", "--binary", fixturePath}},
		{"legacy contract outside redis", []string{"--source", "mysql", "--contract-version", "1", "--version", "2.5.2", "--arch", "amd64", "--binary", fixturePath}},
		{"unknown contract version", []string{"--contract-version", "3", "--version", "2.5.2", "--arch", "amd64", "--binary", fixturePath}},
		{"invalid architecture", []string{"--version", "2.5.2", "--arch", "AMD64", "--binary", fixturePath}},
		{"unknown flag", []string{"--version", "2.5.2", "--arch", "amd64", "--binary", fixturePath, "--force"}},
		{"positional argument", []string{"--version", "2.5.2", "--arch", "amd64", "--binary", fixturePath, "extra"}},
		{"missing binary", []string{"--version", "2.5.2", "--arch", "amd64", "--binary", "testdata/absent"}},
		{"binary is not executable", []string{"--version", "2.5.2", "--arch", "amd64", "--binary", nonExecutable}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "never.tar.gz")
			arguments := append(append([]string{}, test.arguments...), "--output", output)
			_, err := run(arguments)
			var usage usageError
			if !errors.As(err, &usage) {
				t.Fatalf("error = %v, want a usage error", err)
			}
			if _, statErr := os.Stat(output); statErr == nil {
				t.Fatal("a rejected invocation created the archive")
			}
		})
	}
}

func archiveDigest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func archiveMembers(t *testing.T, path string) []string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var members []string
	archive := tar.NewReader(reader)
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Mode != 0o644 && header.Mode != 0o755 {
			t.Fatalf("member %s has mode %o", header.Name, header.Mode)
		}
		members = append(members, header.Name)
	}
	return members
}

func readManifestAndSchema(t *testing.T, path string) ([]byte, []byte) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	found := map[string][]byte{}
	archive := tar.NewReader(reader)
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(archive)
		if err != nil {
			t.Fatal(err)
		}
		found[header.Name] = content
	}
	return found["plugin.json"], found["config.schema.json"]
}
