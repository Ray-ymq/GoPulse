package k6

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestContinuousRequiresFreshHealthyIndependentObservation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "safety.json")
	now := time.Now().UTC()
	for _, test := range []struct {
		status string
		age    time.Duration
		valid  bool
	}{{"ready", time.Second, true}, {"ready", 4 * time.Second, false}, {"stop", 0, false}} {
		data, _ := json.Marshal(safetyStatus{Timestamp: now.Add(-test.age), Status: test.status, Reason: "test"})
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := checkSafety(path, now); (err == nil) != test.valid {
			t.Fatalf("status=%s age=%s err=%v", test.status, test.age, err)
		}
	}
}
