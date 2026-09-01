package middleware

import (
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type fakeTokenVerifier struct {
	userID uint64
	err    error
	token  string
}

func (verifier *fakeTokenVerifier) Verify(token string) (uint64, error) {
	verifier.token = token
	return verifier.userID, verifier.err
}

func TestRequireAuthenticationAddsTypedUserIDToRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	verifier := &fakeTokenVerifier{userID: 42}
	router := gin.New()
	router.Use(RequireAuthentication("session", verifier))
	router.GET("/protected", func(c *gin.Context) {
		identifier, ok := CurrentUserID(c)
		if !ok || identifier != 42 {
			t.Fatalf("CurrentUserID() = %d, %v", identifier, ok)
		}
		c.Status(stdhttp.StatusNoContent)
	})

	request := httptest.NewRequest(stdhttp.MethodGet, "/protected", nil)
	request.AddCookie(&stdhttp.Cookie{Name: "session", Value: "signed-token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != stdhttp.StatusNoContent {
		t.Fatalf("status = %d, want 204", response.Code)
	}
	if verifier.token != "signed-token" {
		t.Fatalf("Verify() token = %q", verifier.token)
	}
}

func TestRequireAuthenticationMapsMissingAndInvalidCookiesWithoutLeakingToken(t *testing.T) {
	for _, test := range []struct {
		name     string
		cookie   *stdhttp.Cookie
		verifier *fakeTokenVerifier
	}{
		{name: "missing cookie", verifier: &fakeTokenVerifier{}},
		{name: "invalid token", cookie: &stdhttp.Cookie{Name: "session", Value: "secret.invalid.token"}, verifier: &fakeTokenVerifier{err: errors.New("signature mismatch")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.Use(RequireAuthentication("session", test.verifier))
			router.GET("/protected", func(c *gin.Context) { c.Status(stdhttp.StatusNoContent) })

			request := httptest.NewRequest(stdhttp.MethodGet, "/protected", nil)
			if test.cookie != nil {
				request.AddCookie(test.cookie)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != stdhttp.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", response.Code)
			}
			body := response.Body.String()
			if !strings.Contains(body, `"code":"authentication_required"`) {
				t.Fatalf("body = %s, want authentication_required", body)
			}
			for _, forbidden := range []string{"secret.invalid.token", "signature mismatch"} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("response leaked %q: %s", forbidden, body)
				}
			}
		})
	}
}
