package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
	"github.com/gin-gonic/gin"
)

const fixedRequestID = "0123456789abcdef0123456789abcdef"

func TestRequestLoggingUsesServerIDAndSafeCompletionMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	logger := logging.Module(logging.New("backend", &output), "http")
	router := gin.New()
	router.Use(RequestID(logger, func() (string, error) { return fixedRequestID, nil }), Access(logger), Recovery(logger))
	router.GET("/items/:itemId", RequireAuthentication("session", fixedVerifier{userID: 42}), func(c *gin.Context) {
		response.Error(c, apperror.New(apperror.CodeValidationFailed, "invalid item"))
	})

	request := httptest.NewRequest(stdhttp.MethodGet, "/items/secret-path?token=secret-query", nil)
	request.Header.Set(requestIDHeader, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	request.AddCookie(&stdhttp.Cookie{Name: "session", Value: "secret-cookie"})
	result := httptest.NewRecorder()
	router.ServeHTTP(result, request)

	if result.Header().Get(requestIDHeader) != fixedRequestID {
		t.Fatalf("X-Request-ID = %q", result.Header().Get(requestIDHeader))
	}
	records := decodeRecords(t, output.Bytes())
	if len(records) != 1 {
		t.Fatalf("records = %#v, want one completion", records)
	}
	record := records[0]
	if record["message"] != "http request completed" || record["request_id"] != fixedRequestID || record["route"] != "/items/:itemId" || record["method"] != "GET" || record["status"] != float64(400) || record["level"] != "warn" || record["user_id"] != float64(42) || record["error_code"] != "validation_failed" {
		t.Fatalf("completion record = %#v", record)
	}
	for _, forbidden := range []string{"secret-path", "secret-query", "secret-cookie", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatalf("log leaked %q: %s", forbidden, output.String())
		}
	}
}

func TestRequestLoggingRecoversPanicBeforeCompletion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	logger := logging.Module(logging.New("backend", &output), "http")
	router := gin.New()
	router.Use(RequestID(logger, func() (string, error) { return fixedRequestID, nil }), Access(logger), Recovery(logger))
	router.GET("/panic", func(*gin.Context) { panic("secret panic value") })

	result := httptest.NewRecorder()
	router.ServeHTTP(result, httptest.NewRequest(stdhttp.MethodGet, "/panic", nil))

	if result.Code != stdhttp.StatusInternalServerError || result.Body.String() != `{"error":{"code":"internal_error","message":"an internal error occurred"}}` {
		t.Fatalf("response = %d %s", result.Code, result.Body.String())
	}
	records := decodeRecords(t, output.Bytes())
	if len(records) != 2 || records[0]["message"] != "http panic recovered" || records[1]["message"] != "http request completed" || records[1]["status"] != float64(500) || records[1]["level"] != "error" || records[1]["error_code"] != "internal_error" {
		t.Fatalf("records = %#v", records)
	}
	if strings.Contains(output.String(), "secret panic value") || strings.Contains(output.String(), "goroutine") {
		t.Fatalf("panic details leaked: %s", output.String())
	}
}

func TestRequestLoggingRecoversPanicAfterResponseCommitWithoutMixedPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	logger := logging.Module(logging.New("backend", &output), "http")
	router := gin.New()
	router.Use(RequestID(logger, func() (string, error) { return fixedRequestID, nil }), Access(logger), Recovery(logger))
	router.GET("/panic-after-write", func(c *gin.Context) {
		c.String(stdhttp.StatusAccepted, "partial-secret")
		panic("secret panic value")
	})

	result := httptest.NewRecorder()
	router.ServeHTTP(result, httptest.NewRequest(stdhttp.MethodGet, "/panic-after-write", nil))

	if result.Code != stdhttp.StatusAccepted || result.Body.String() != "partial-secret" {
		t.Fatalf("response = %d %q", result.Code, result.Body.String())
	}
	records := decodeRecords(t, output.Bytes())
	if len(records) != 2 {
		t.Fatalf("records = %#v, want panic and completion", records)
	}
	panicRecord, completion := records[0], records[1]
	if panicRecord["message"] != "http panic recovered" || panicRecord["level"] != "error" || panicRecord["error_code"] != "internal_error" || panicRecord["response_committed"] != true {
		t.Fatalf("panic record = %#v", panicRecord)
	}
	if completion["message"] != "http request completed" || completion["status"] != float64(stdhttp.StatusAccepted) || completion["level"] != "error" || completion["error_code"] != "internal_error" || completion["panic_recovered"] != true || completion["response_committed"] != true {
		t.Fatalf("completion record = %#v", completion)
	}
	if strings.Contains(result.Body.String(), `"error"`) || strings.Contains(output.String(), "secret panic value") || strings.Contains(output.String(), "goroutine") {
		t.Fatalf("panic response or log leaked unsafe details: response=%q log=%s", result.Body.String(), output.String())
	}
}

func TestRequestIDFailureReturnsSafeErrorWithoutForgedID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	logger := logging.Module(logging.New("backend", &output), "http")
	router := gin.New()
	router.Use(RequestID(logger, func() (string, error) { return "", errors.New("entropy secret") }), Access(logger), Recovery(logger))
	router.GET("/", func(c *gin.Context) { c.Status(stdhttp.StatusNoContent) })

	request := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
	request.Header.Set(requestIDHeader, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	result := httptest.NewRecorder()
	router.ServeHTTP(result, request)

	if result.Code != stdhttp.StatusInternalServerError || result.Header().Get(requestIDHeader) != "" {
		t.Fatalf("response = %d, request ID = %q", result.Code, result.Header().Get(requestIDHeader))
	}
	records := decodeRecords(t, output.Bytes())
	if len(records) != 1 || records[0]["message"] != "request id generation failed" || records[0]["error_code"] != "internal_error" {
		t.Fatalf("records = %#v", records)
	}
	if _, exists := records[0]["request_id"]; exists || strings.Contains(output.String(), "entropy secret") || strings.Contains(output.String(), "bbbbbbbb") {
		t.Fatalf("unsafe generation failure log: %s", output.String())
	}
}

func TestRandomRequestIDFormat(t *testing.T) {
	first, err := RandomRequestID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := RandomRequestID()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 32 || len(second) != 32 || first == second || strings.ToLower(first) != first {
		t.Fatalf("request IDs = %q, %q", first, second)
	}
}

type fixedVerifier struct{ userID uint64 }

func (verifier fixedVerifier) Verify(string) (uint64, error) { return verifier.userID, nil }

func decodeRecords(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	var records []map[string]any
	for decoder.More() {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			t.Fatalf("decode log: %v", err)
		}
		records = append(records, record)
	}
	return records
}
