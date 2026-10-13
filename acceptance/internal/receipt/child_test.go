package receipt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteChildReceiptUsesPrivateAtomicFileAndValidatesIdentity(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "children", "V12.json")
	child := ChildReceipt{
		CaseID: "V12", TaskID: "123e4567-e89b-42d3-a456-426614174000", Status: "canceled",
		Identity:  V1Identity{DataEpoch: "123e4567-e89b-42d3-a456-426614174001", CommandID: "123e4567-e89b-42d3-a456-426614174002", EventID: "123e4567-e89b-42d3-a456-426614174003", PostID: 42},
		StartedAt: "2026-10-13T01:00:00Z", CancelRequestedAt: "2026-10-13T01:00:01Z", FinishedAt: "2026-10-13T01:00:02Z",
		CompletedStages: []string{"started", "cancelled"}, CleanupRemoved: true, OwnedResource: "v1-v12-child-123e4567-e89b-42d3-a456-426614174000",
	}
	if err := WriteChildReceipt(path, child); err != nil {
		t.Fatalf("WriteChildReceipt() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("child receipt permissions = %o, want 600", info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), `"status": "canceled"`) {
		t.Fatalf("child receipt = %q, error = %v", raw, err)
	}
	child.Identity.PostID = 0
	if err := WriteChildReceipt(filepath.Join(root, "invalid.json"), child); err == nil {
		t.Fatal("accepted child receipt without a business identity")
	}
}
