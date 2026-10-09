package contracts

import "testing"

func TestProjectNameContract(t *testing.T) {
	for _, value := range []string{"gopulse-accept-012345abcdef", "gopulse", "../gopulse", "GOPULSE-accept-012345abcdef", "gopulse-accept-012345abcdeg"} {
		if value == "gopulse-accept-012345abcdef" {
			if !ValidProjectName(value) {
				t.Fatalf("valid project was rejected: %s", value)
			}
			continue
		}
		if ValidProjectName(value) {
			t.Fatalf("unsafe project was accepted: %s", value)
		}
	}
}

func TestLoopbackAndRedactionContracts(t *testing.T) {
	if !ValidLoopback("127.0.0.1") || ValidLoopback("0.0.0.0") || ValidLoopback("localhost") {
		t.Fatal("loopback contract mismatch")
	}
	if redacted := Redact("token=secret", "secret"); redacted != "token=[REDACTED]" {
		t.Fatalf("redaction = %q", redacted)
	}
}
