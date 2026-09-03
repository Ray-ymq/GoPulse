package response

import (
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/gin-gonic/gin"
)

func TestDataAndPageResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("data", func(t *testing.T) {
		response := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(response)
		Data(context, stdhttp.StatusCreated, gin.H{"id": 7})
		assertBody(t, response, stdhttp.StatusCreated, `{"data":{"id":7}}`)
	})

	t.Run("page", func(t *testing.T) {
		cursor := "next-token"
		response := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(response)
		Page(context, stdhttp.StatusOK, []int{1, 2}, &cursor)
		assertBody(t, response, stdhttp.StatusOK, `{"data":[1,2],"meta":{"next_cursor":"next-token"}}`)
	})

	t.Run("last page", func(t *testing.T) {
		response := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(response)
		Page(context, stdhttp.StatusOK, []int{}, nil)
		assertBody(t, response, stdhttp.StatusOK, `{"data":[],"meta":{"next_cursor":null}}`)
	})
}

func TestErrorResponseMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		err        error
		status     int
		expected   string
		mustNotSee string
	}{
		{name: "validation", err: apperror.New(apperror.CodeValidationFailed, "invalid input"), status: 400, expected: `{"error":{"code":"validation_failed","message":"invalid input"}}`},
		{name: "authentication", err: apperror.New(apperror.CodeAuthenticationRequired, "sign in required"), status: 401, expected: `{"error":{"code":"authentication_required","message":"sign in required"}}`},
		{name: "conflict", err: apperror.New(apperror.CodeUsernameConflict, "username already exists"), status: 409, expected: `{"error":{"code":"username_conflict","message":"username already exists"}}`},
		{name: "not found", err: apperror.New(apperror.CodePostNotFound, "post not found"), status: 404, expected: `{"error":{"code":"post_not_found","message":"post not found"}}`},
		{name: "unknown", err: errors.New("sql: password=secret dsn=user:secret@tcp"), status: 500, expected: `{"error":{"code":"internal_error","message":"an internal error occurred"}}`, mustNotSee: "secret"},
		{name: "wrapped internal", err: apperror.WrapInternal(errors.New("jwt-secret")), status: 500, expected: `{"error":{"code":"internal_error","message":"an internal error occurred"}}`, mustNotSee: "jwt-secret"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			Error(context, test.err)
			assertBody(t, response, test.status, test.expected)
			code, ok := ErrorCode(context)
			if !ok || string(code) == "" {
				t.Fatalf("ErrorCode() = %q, %v", code, ok)
			}
			if test.mustNotSee != "" && contains(response.Body.String(), test.mustNotSee) {
				t.Fatalf("response leaked sensitive detail %q: %s", test.mustNotSee, response.Body.String())
			}
		})
	}
}

func assertBody(t *testing.T, response *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d", response.Code, status)
	}
	if response.Body.String() != body {
		t.Fatalf("body = %s, want %s", response.Body.String(), body)
	}
}

func contains(value, substring string) bool {
	for index := 0; index+len(substring) <= len(value); index++ {
		if value[index:index+len(substring)] == substring {
			return true
		}
	}
	return false
}
