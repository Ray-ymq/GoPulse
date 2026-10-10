package stack

import (
	"os"
	"strings"
	"testing"
)

func TestProjectNameContract(t *testing.T) {
	for _, value := range []string{"gopulse", "gopulse_dev", "a" + strings.Repeat("b", 62)} {
		if !projectPattern.MatchString(value) {
			t.Fatalf("valid project %q rejected", value)
		}
	}
	for _, value := range []string{"", "Gopulse", "../gopulse", "-gopulse", strings.Repeat("a", 64)} {
		if projectPattern.MatchString(value) {
			t.Fatalf("unsafe project %q accepted", value)
		}
	}
}

func TestNonEmptyLines(t *testing.T) {
	got := nonEmptyLines("\nfirst\n\nsecond\n")
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("lines = %#v", got)
	}
}

func TestValidatePort(t *testing.T) {
	for _, value := range []string{"1", "8080", "65535"} {
		if err := validatePort("HTTP_PORT", value); err != nil {
			t.Fatalf("valid port %q rejected: %v", value, err)
		}
	}
	for _, value := range []string{"", "0", "65536", "8080:8081", "-1"} {
		if err := validatePort("HTTP_PORT", value); err == nil {
			t.Fatalf("invalid port %q accepted", value)
		}
	}
}

func TestBackendPortOverride(t *testing.T) {
	ctx := &context{httpPort: "8080"}
	if err := ctx.prepareBackendPortOverride(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctx.removeBackendPortOverride)
	data, err := os.ReadFile(ctx.overrideFile)
	if err != nil {
		t.Fatal(err)
	}
	want := "services:\n  backend:\n    ports:\n      - \"127.0.0.1:8080:8080\"\n"
	if string(data) != want {
		t.Fatalf("override = %q, want %q", data, want)
	}
}
