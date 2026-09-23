package load

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
