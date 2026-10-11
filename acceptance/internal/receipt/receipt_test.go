package receipt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicPublishesPrivateSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "receipt.json")
	document := New("gopulse-accept-012345abcdef", "2.5.5", "revision")
	if err := WriteAtomic(path, document); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("receipt mode = %o", info.Mode().Perm())
	}
	var decoded Document
	if raw, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	} else if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Schema != Schema || decoded.Project == "" || decoded.FinishedAt == "" {
		t.Fatalf("invalid receipt: %+v", decoded)
	}
}
