package migrations

import (
	"testing"
	"testing/fstest"
)

func TestInventory(t *testing.T) {
	if target, err := Target(); err != nil || target != 13 {
		t.Fatalf("target=%d err=%v", target, err)
	}
	for _, names := range [][]string{
		{"000001_a.up.sql"},
		{"000001_a.up.sql", "000001_b.down.sql"},
		{"000002_a.up.sql", "000002_a.down.sql"},
		{"000001_a.up.sql", "000001_a.down.sql", "000001_b.up.sql"},
	} {
		files := fstest.MapFS{}
		for _, name := range names {
			files[name] = &fstest.MapFile{Data: []byte("SELECT 1;")}
		}
		if _, err := validateInventory(files); err == nil {
			t.Fatalf("accepted %v", names)
		}
	}
}
