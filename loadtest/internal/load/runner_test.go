package load

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestExecuteRequestClassifiesTimeoutAndExplicitRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/slow":
			time.Sleep(50 * time.Millisecond)
			writer.WriteHeader(http.StatusOK)
		case "/overloaded":
			writer.Header().Set("Retry-After", "1")
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = writer.Write([]byte(`{"error":{"code":"server_overloaded"}}`))
		case "/error":
			writer.Header().Set("X-Request-ID", "0123456789abcdef0123456789abcdef")
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = writer.Write([]byte(`{"error":{"code":"internal_error"}}`))
		default:
			writer.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	client := server.Client()
	scheduled := time.Now()
	request := Request{Category: CategoryRead, Method: http.MethodGet, Template: "GET /slow", Path: "/slow", ExpectedStatuses: statuses(http.StatusOK)}
	timeout := executeRequest(context.Background(), client, server.URL, "session", "token", request, scheduled, 10*time.Millisecond)
	if !timeout.timeout {
		t.Fatalf("timeout=%+v", timeout)
	}
	rejected := executeRequest(context.Background(), client, server.URL, "session", "token", Request{Category: CategoryContentWrite, Method: http.MethodGet, Template: "GET /overloaded", Path: "/overloaded", ExpectedStatuses: statuses(http.StatusOK)}, scheduled, time.Second)
	if !rejected.explicitReject || rejected.timeout || rejected.transportFailure {
		t.Fatalf("rejected=%+v", rejected)
	}
	serverError := executeRequest(context.Background(), client, server.URL, "session", "token", Request{Category: CategoryRead, Method: http.MethodGet, Template: "GET /error", Path: "/error", ExpectedStatuses: statuses(http.StatusOK)}, scheduled, time.Second)
	if serverError.status != http.StatusInternalServerError || serverError.requestID != "0123456789abcdef0123456789abcdef" || serverError.errorCode != "internal_error" || serverError.completedAt.IsZero() {
		t.Fatalf("serverError=%+v", serverError)
	}
}

func TestExplicitRejectClassificationRecognizesBackendBusyWithoutMergingDependencyErrors(t *testing.T) {
	if !isExplicitReject(http.StatusServiceUnavailable, "", []byte(`{"error":{"code":"backend_busy"}}`)) {
		t.Fatal("backend_busy was not classified as an explicit rejection")
	}
	if isExplicitReject(http.StatusServiceUnavailable, "", []byte(`{"error":{"code":"search_unavailable"}}`)) {
		t.Fatal("dependency unavailability was classified as an explicit rejection")
	}
}

func TestDiagnosticReportSeparatesOneSecondWindowsAndRetainsServerErrorRequestID(t *testing.T) {
	started := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	diagnostics := newDiagnosticAccumulator(started)
	diagnostics.add("steady", requestResult{
		category: CategoryRead, route: "GET /api/v1/posts/:postId", method: http.MethodGet,
		status: http.StatusOK, latencyMS: 10, completedAt: started.Add(100 * time.Millisecond),
	})
	diagnostics.add("steady", requestResult{
		category: CategoryContentWrite, route: "DELETE /api/v1/posts/:postId", method: http.MethodDelete,
		status: http.StatusInternalServerError, latencyMS: 250, completedAt: started.Add(1200 * time.Millisecond),
		requestID: "0123456789abcdef0123456789abcdef", errorCode: "internal_error",
	})
	report := diagnostics.report(started.Add(2 * time.Second))
	if report.WindowSeconds != 1 || len(report.Windows) != 2 || report.Windows[0].Sequence != 0 || report.Windows[1].Sequence != 1 {
		t.Fatalf("report=%+v", report)
	}
	if len(report.ServerErrors) != 1 || report.ServerErrors[0].RequestID != "0123456789abcdef0123456789abcdef" || report.ServerErrors[0].ErrorCode != "internal_error" {
		t.Fatalf("server errors=%+v", report.ServerErrors)
	}
	if report.Windows[0].Latency.P95MS != 10 || report.Windows[1].Latency.P95MS != 250 {
		t.Fatalf("window latency=%+v", report.Windows)
	}
}

func TestDiagnosticReportUsesEmptyServerErrorList(t *testing.T) {
	started := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	report := newDiagnosticAccumulator(started).report(started.Add(time.Second))
	if report.ServerErrors == nil || len(report.ServerErrors) != 0 {
		t.Fatalf("server errors=%#v", report.ServerErrors)
	}
}

func TestRunAgainstMockServerProducesSanitizedReport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v1/auth/login" {
			http.SetCookie(writer, &http.Cookie{Name: "gopulse_session", Value: "private-cookie", Path: "/"})
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte(`{"data":{"id":1}}`))
			return
		}
		if request.Method == http.MethodPost {
			writer.WriteHeader(http.StatusCreated)
			return
		}
		if request.Method == http.MethodPatch {
			writer.WriteHeader(http.StatusOK)
			return
		}
		if request.Method == http.MethodDelete || request.Method == http.MethodPut {
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()
	corpus := testCorpus()
	credentials := Credentials{SchemaVersion: CredentialsSchemaVersion, Password: "private-password", Users: corpus.Users}
	report, err := Run(context.Background(), Config{
		BaseURL: server.URL, CookieName: "gopulse_session", Corpus: corpus, Credentials: credentials,
		VirtualUsers: 4, RequestTimeout: time.Second,
		Steady: 200 * time.Millisecond, Burst: 50 * time.Millisecond, SteadyRPS: 40, BurstRPS: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Total.Requests == 0 || report.Total.Succeeded != report.Total.Requests {
		t.Fatalf("report=%+v", report)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private-password") || strings.Contains(string(encoded), "private-cookie") {
		t.Fatalf("report leaked credentials: %s", encoded)
	}
}

func TestSchedulePhaseAssignsSlotsDeterministicallyToVirtualUsers(t *testing.T) {
	jobs := []chan scheduledSlot{make(chan scheduledSlot, 10), make(chan scheduledSlot, 10)}
	slotIndex := uint64(0)
	scheduled, dropped, _, err := schedulePhase(context.Background(), phaseSpec{
		name: "steady", duration: 20 * time.Millisecond, rps: 500,
	}, jobs, &slotIndex)
	if err != nil {
		t.Fatal(err)
	}
	if scheduled != 10 || dropped != 0 || slotIndex != 10 {
		t.Fatalf("scheduled=%d dropped=%d slotIndex=%d", scheduled, dropped, slotIndex)
	}
	for virtualUser := range jobs {
		close(jobs[virtualUser])
		for slot := range jobs[virtualUser] {
			if int(slot.index%uint64(len(jobs))) != virtualUser {
				t.Fatalf("slot %d assigned to virtual user %d", slot.index, virtualUser)
			}
		}
	}
}

func TestScheduleCapacityWindowRetainsOpenLoopStageAndWindowIdentity(t *testing.T) {
	jobs := []chan capacitySlot{make(chan capacitySlot, 16), make(chan capacitySlot, 16)}
	var slotIndex uint64
	scheduled, dropped, _, err := scheduleCapacityWindow(context.Background(), 100, 0.05, jobs, &slotIndex, 2, "measurement")
	if err != nil {
		t.Fatal(err)
	}
	if scheduled != 5 || dropped != 0 || slotIndex != 5 {
		t.Fatalf("scheduled=%d dropped=%d slot=%d", scheduled, dropped, slotIndex)
	}
	for _, channel := range jobs {
		close(channel)
		for slot := range channel {
			if slot.stage != 2 || slot.window != "measurement" {
				t.Fatalf("slot=%+v", slot)
			}
		}
	}
}

func TestScheduleCapacityWindowStopsOnlyOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	jobs := []chan capacitySlot{make(chan capacitySlot, 1)}
	_, _, _, err := scheduleCapacityWindow(ctx, 100, 1, jobs, new(uint64), 0, "warmup")
	if err == nil {
		t.Fatal("cancelled capacity window was accepted")
	}
	for _, channel := range jobs {
		close(channel)
	}
}

func TestRunCapacityUsesFrozenStagesAndWritesOneIndependentReport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v1/auth/login" {
			http.SetCookie(writer, &http.Cookie{Name: "gopulse_session", Value: "test-session", Path: "/"})
			writer.WriteHeader(http.StatusOK)
			return
		}
		switch request.Method {
		case http.MethodPost:
			writer.WriteHeader(http.StatusCreated)
		case http.MethodPatch:
			writer.WriteHeader(http.StatusOK)
		case http.MethodPut, http.MethodDelete:
			writer.WriteHeader(http.StatusNoContent)
		default:
			writer.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()
	profile, digest, err := LoadProfile("../../capacity-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	for index := range profile.Stages {
		profile.Stages[index].WarmupSeconds = 0.04
		profile.Stages[index].MeasurementSeconds = 0.04
		profile.Stages[index].RecoverySeconds = 0.01
	}
	users := make([]User, profile.Workload.VirtualUsers)
	editable := make([][]uint64, profile.Workload.VirtualUsers)
	deletable := make([][]uint64, profile.Workload.VirtualUsers)
	for index := range users {
		users[index] = User{ID: uint64(index + 1), Username: "user" + strconv.Itoa(index+1)}
		editable[index] = []uint64{uint64(index + 1)}
		deletable[index] = []uint64{uint64(index + 1)}
	}
	corpus := Corpus{SchemaVersion: "gopulse.phase18.recipe.v1", Seed: profile.Recipe.Seed, Users: users, ReadPostIDs: []uint64{100}, InteractionPostIDs: []uint64{100}, EditablePostIDs: editable, DeletePostIDs: deletable}
	credentials := Credentials{SchemaVersion: CredentialsSchemaVersion, Password: "password", Users: users}
	reportPath := filepath.Join(t.TempDir(), "load-report.json")
	report, err := RunCapacity(context.Background(), CapacityRunConfig{
		BaseURL: server.URL, Corpus: corpus, Credentials: credentials, Profile: profile,
		ProfileDigest: digest, Candidate: CandidateBinding{Version: "2.1.3", Revision: strings.Repeat("a", 40), ManifestSHA256: "sha256:" + strings.Repeat("b", 64)}, Repeat: 1, ReportPath: reportPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.ExecutionStatus != "complete" || len(report.Stages) != 4 || report.Stages[3].TargetRPS != 200 || report.Repeat.Number != 1 || report.Repeat.Total != 3 {
		t.Fatalf("report=%+v", report)
	}
	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("report was not written: %v", err)
	}
}
