package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/loadtest/internal/load"
)

func TestRunAcceptsClosedLoopSaturationFlags(t *testing.T) {
	const virtualUsers = 4
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v1/auth/login" {
			http.SetCookie(writer, &http.Cookie{Name: "session", Value: "test-cookie", Path: "/"})
			writer.WriteHeader(http.StatusOK)
			return
		}
		current := inFlight.Add(1)
		for previous := maxInFlight.Load(); current > previous; previous = maxInFlight.Load() {
			if maxInFlight.CompareAndSwap(previous, current) {
				break
			}
		}
		time.Sleep(3 * time.Millisecond)
		inFlight.Add(-1)
		switch request.Method {
		case http.MethodPost:
			writer.WriteHeader(http.StatusCreated)
		case http.MethodPut, http.MethodDelete:
			writer.WriteHeader(http.StatusNoContent)
		default:
			writer.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	users := make([]load.User, virtualUsers)
	editable := make([][]uint64, virtualUsers)
	deletable := make([][]uint64, virtualUsers)
	for index := range users {
		users[index] = load.User{ID: uint64(index + 1), Username: "user" + string(rune('a'+index))}
		editable[index] = []uint64{uint64(100 + index)}
		deletable[index] = []uint64{uint64(200 + index)}
	}
	corpus := load.Corpus{
		SchemaVersion: "test", Seed: 18, Users: users,
		ReadPostIDs: []uint64{1, 2}, InteractionPostIDs: []uint64{3, 4},
		EditablePostIDs: editable, DeletePostIDs: deletable,
	}
	credentials := load.Credentials{
		SchemaVersion: load.CredentialsSchemaVersion,
		Password:      "private-test-password",
		Users:         users,
	}
	temporary := t.TempDir()
	corpusPath := filepath.Join(temporary, "corpus.json")
	credentialsPath := filepath.Join(temporary, "credentials.json")
	reportPath := filepath.Join(temporary, "report.json")
	if err := writeJSON(corpusPath, corpus); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(credentialsPath, credentials); err != nil {
		t.Fatal(err)
	}

	code := run([]string{
		"--base-url", server.URL,
		"--corpus", corpusPath,
		"--credentials", credentialsPath,
		"--report", reportPath,
		"--diagnostic-report", filepath.Join(temporary, "diagnostic.json"),
		"--cookie-name", "session",
		"--vus", "4",
		"--saturate",
		"--active-workers", "2",
		"--warmup", "40ms",
		"--steady", "70ms",
		"--burst", "0s",
		"--request-timeout", "1s",
	})
	if code != 0 {
		t.Fatalf("run() exit code = %d", code)
	}

	encoded, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		MeasurementMode string  `json:"measurement_mode"`
		VirtualUsers    int     `json:"virtual_users"`
		ActiveWorkers   int     `json:"active_workers"`
		SteadyTargetRPS float64 `json:"steady_target_rps"`
		BurstTargetRPS  float64 `json:"burst_target_rps"`
		Total           struct {
			Requests  uint64 `json:"requests"`
			Succeeded uint64 `json:"succeeded"`
			Errors    uint64 `json:"errors"`
			Timeouts  uint64 `json:"timeouts"`
		} `json:"total"`
		Phases []struct {
			Name              string  `json:"name"`
			TargetRPS         float64 `json:"target_rps"`
			ScheduledSlots    uint64  `json:"scheduled_slots"`
			CompletedRequests uint64  `json:"completed_requests"`
		} `json:"phases"`
	}
	if err := json.Unmarshal(encoded, &report); err != nil {
		t.Fatal(err)
	}
	if report.MeasurementMode != "closed_loop" || report.VirtualUsers != virtualUsers || report.ActiveWorkers != 2 {
		t.Fatalf("report mode or concurrency = %+v", report)
	}
	if report.SteadyTargetRPS != 0 || report.BurstTargetRPS != 0 {
		t.Fatalf("closed-loop report has offered-rate targets: %+v", report)
	}
	if report.Total.Requests == 0 || report.Total.Succeeded != report.Total.Requests || report.Total.Errors != 0 || report.Total.Timeouts != 0 {
		t.Fatalf("closed-loop requests did not complete successfully: %+v", report.Total)
	}
	if len(report.Phases) != 2 || report.Phases[0].Name != "warmup" || report.Phases[1].Name != "steady" {
		t.Fatalf("closed-loop phases = %+v", report.Phases)
	}
	for _, phase := range report.Phases {
		if phase.TargetRPS != 0 || phase.ScheduledSlots != 0 || phase.CompletedRequests == 0 {
			t.Fatalf("phase has fixed-rate slots or no completed requests: %+v", phase)
		}
	}
	if current := maxInFlight.Load(); current < 1 || current > 2 {
		t.Fatalf("observed active requests = %d, want a maximum of two", current)
	}
}

func writeJSON(path string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, encoded, 0o600)
}
