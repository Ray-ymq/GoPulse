package control

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Reproduces the first real recovery run's dated ES index rejection.
func TestSearchDatedIndexIdentity(t *testing.T) {
	if !searchIdentity.MatchString("gopulse-logs-v1-2026.09.13") {
		t.Fatal("real dated index rejected")
	}
	if searchIdentity.MatchString("gopulse-logs/../../other") {
		t.Fatal("non-index path accepted")
	}
}

func TestPendingCiphertextCleanupIsOperationScoped(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	operation := strings.Repeat("a", 64)
	c := &Controller{dir: dir, state: State{Operation: operation}}
	own := filepath.Join(dir, ".backup-pending-"+operation)
	other := filepath.Join(dir, ".backup-pending-"+strings.Repeat("b", 64))
	for _, p := range []string{own, other} {
		if err := os.WriteFile(p, []byte("ciphertext placeholder"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.cleanupPendingCiphertext(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(own); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("own interrupted output not removed")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("another operation was changed")
	}
	if err := os.Symlink(other, own); err != nil {
		t.Fatal(err)
	}
	if c.cleanupPendingCiphertext() == nil {
		t.Fatal("unsafe pending link accepted")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("link target changed")
	}
}

func TestValidKafkaOffsetsSupportsCurrentPartitionTopology(t *testing.T) {
	offsets := []kafkaOffset{
		{Topic: "gopulse-observability-v1", Partition: 0, Committed: 10, End: 10},
		{Topic: "gopulse-observability-v1", Partition: 1, Committed: 11, End: 11},
		{Topic: "gopulse-observability-v1", Partition: 2, Committed: 12, End: 12},
		{Topic: "gopulse-observability-v1", Partition: 3, Committed: 13, End: 13},
	}
	if !validKafkaOffsets(offsets, true) {
		t.Fatal("current four-partition drained topology was rejected")
	}
	if validKafkaOffsets(append([]kafkaOffset(nil), offsets[:3]...), true) == false {
		t.Fatal("a valid bounded prefix should remain valid")
	}
}

func TestValidKafkaOffsetsRejectsGapsAndUndrainedPartitions(t *testing.T) {
	offsets := []kafkaOffset{
		{Topic: "gopulse-observability-v1", Partition: 0, Committed: 10, End: 11},
		{Topic: "gopulse-observability-v1", Partition: 2, Committed: 12, End: 12},
	}
	if validKafkaOffsets(offsets, false) {
		t.Fatal("partition gap was accepted")
	}
	if validKafkaOffsets([]kafkaOffset{{Topic: "gopulse-observability-v1", Partition: 0, Committed: 10, End: 11}}, true) {
		t.Fatal("undrained partition was accepted")
	}
}
