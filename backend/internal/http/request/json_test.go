package request

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/gin-gonic/gin"
)

type requestPayload struct {
	Name string `json:"name"`
}

func TestDecodeJSONAcceptsOneKnownObject(t *testing.T) {
	context := testContext(`{"name":"gopulse"}`)
	var payload requestPayload
	if err := DecodeJSON(context, &payload); err != nil {
		t.Fatalf("DecodeJSON() error = %v", err)
	}
	if payload.Name != "gopulse" {
		t.Fatalf("Name = %q, want gopulse", payload.Name)
	}
}

func TestDecodeJSONRejectsUnsafeBodies(t *testing.T) {
	oversized := `{"name":"` + strings.Repeat("x", int(DefaultJSONBodyLimit)) + `"}`
	tests := []struct {
		name string
		body string
	}{
		{name: "empty", body: ""},
		{name: "whitespace", body: "  \r\n\t"},
		{name: "unknown field", body: `{"name":"gopulse","secret":"value"}`},
		{name: "multiple values", body: `{"name":"one"}{"name":"two"}`},
		{name: "invalid json", body: `{"name":`},
		{name: "oversized", body: oversized},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context := testContext(test.body)
			var payload requestPayload
			err := DecodeJSON(context, &payload)
			appError, ok := apperror.As(err)
			if !ok || appError.Code != apperror.CodeValidationFailed {
				t.Fatalf("DecodeJSON() error = %#v, want validation_failed", err)
			}
		})
	}
}

func testContext(body string) *gin.Context {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest("POST", "/api/v1/test", strings.NewReader(body))
	return context
}
