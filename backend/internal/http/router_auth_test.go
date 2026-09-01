package http

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/auth"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
	"github.com/gin-gonic/gin"
)

type fakeAuthApplication struct {
	register    func(context.Context, auth.Credentials) (user.Public, string, error)
	login       func(context.Context, auth.Credentials) (user.Public, string, error)
	currentUser func(context.Context, uint64) (user.Public, error)
}

func (application *fakeAuthApplication) Register(ctx context.Context, credentials auth.Credentials) (user.Public, string, error) {
	return application.register(ctx, credentials)
}

func (application *fakeAuthApplication) Login(ctx context.Context, credentials auth.Credentials) (user.Public, string, error) {
	return application.login(ctx, credentials)
}

func (application *fakeAuthApplication) CurrentUser(ctx context.Context, userID uint64) (user.Public, error) {
	return application.currentUser(ctx, userID)
}

type acceptingVerifier struct {
	userID uint64
}

func (verifier acceptingVerifier) Verify(string) (uint64, error) {
	return verifier.userID, nil
}

func TestAuthenticationRoutesExposeStableContracts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	createdAt := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	publicUser := user.Public{ID: 17, Username: "alice", CreatedAt: createdAt}
	secretToken := "header.sensitive-signature.payload"
	application := &fakeAuthApplication{
		register: func(_ context.Context, credentials auth.Credentials) (user.Public, string, error) {
			if credentials.Username != "alice" || credentials.Password != "plain-password" {
				t.Fatalf("Register() credentials = %#v", credentials)
			}
			return publicUser, secretToken, nil
		},
		login: func(context.Context, auth.Credentials) (user.Public, string, error) {
			return publicUser, secretToken, nil
		},
		currentUser: func(_ context.Context, userID uint64) (user.Public, error) {
			if userID != 17 {
				t.Fatalf("CurrentUser() ID = %d, want 17", userID)
			}
			return publicUser, nil
		},
	}
	cookies := auth.NewCookieManager("session", false, 2*time.Hour, func() time.Time { return createdAt })
	handler := auth.NewHandler(application, cookies)
	router := NewRouter(Dependencies{}, APIRoutes{
		Auth:           handler,
		Authentication: middleware.RequireAuthentication(cookies.Name(), acceptingVerifier{userID: 17}),
	})

	registerResponse := performJSONRequest(router, stdhttp.MethodPost, "/api/v1/auth/register", `{"username":"alice","password":"plain-password"}`, nil)
	if registerResponse.Code != stdhttp.StatusCreated {
		t.Fatalf("register status = %d body=%s", registerResponse.Code, registerResponse.Body.String())
	}
	assertJSONEqual(t, registerResponse.Body.String(), `{"data":{"id":17,"username":"alice","created_at":"2026-09-01T12:00:00Z"}}`)
	assertSessionCookie(t, registerResponse, "session", secretToken, 7200)
	assertNoSensitiveResponseData(t, registerResponse.Body.String(), "plain-password", secretToken, "$2a$10$password-hash")

	loginResponse := performJSONRequest(router, stdhttp.MethodPost, "/api/v1/auth/login", `{"username":"alice","password":"plain-password"}`, nil)
	if loginResponse.Code != stdhttp.StatusOK {
		t.Fatalf("login status = %d body=%s", loginResponse.Code, loginResponse.Body.String())
	}
	assertSessionCookie(t, loginResponse, "session", secretToken, 7200)
	assertNoSensitiveResponseData(t, loginResponse.Body.String(), "plain-password", secretToken)

	meResponse := performJSONRequest(router, stdhttp.MethodGet, "/api/v1/users/me", "", &stdhttp.Cookie{Name: "session", Value: secretToken})
	if meResponse.Code != stdhttp.StatusOK {
		t.Fatalf("me status = %d body=%s", meResponse.Code, meResponse.Body.String())
	}
	assertJSONEqual(t, meResponse.Body.String(), `{"data":{"id":17,"username":"alice","created_at":"2026-09-01T12:00:00Z"}}`)

	logoutResponse := performJSONRequest(router, stdhttp.MethodPost, "/api/v1/auth/logout", "", nil)
	if logoutResponse.Code != stdhttp.StatusNoContent || logoutResponse.Body.Len() != 0 {
		t.Fatalf("logout status=%d body=%q, want empty 204", logoutResponse.Code, logoutResponse.Body.String())
	}
	assertSessionCookie(t, logoutResponse, "session", "", -1)
}

func TestAuthenticationRoutesMapErrorsAndClearDeletedUserCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	application := &fakeAuthApplication{
		register: func(context.Context, auth.Credentials) (user.Public, string, error) {
			return user.Public{}, "", apperror.New(apperror.CodeUsernameConflict, "username is already in use")
		},
		login: func(context.Context, auth.Credentials) (user.Public, string, error) {
			return user.Public{}, "", apperror.New(apperror.CodeInvalidCredentials, "invalid username or password")
		},
		currentUser: func(context.Context, uint64) (user.Public, error) {
			return user.Public{}, apperror.New(apperror.CodeAuthenticationRequired, "authentication is required")
		},
	}
	cookies := auth.NewCookieManager("session", false, 2*time.Hour, time.Now)
	router := NewRouter(Dependencies{}, APIRoutes{
		Auth:           auth.NewHandler(application, cookies),
		Authentication: middleware.RequireAuthentication(cookies.Name(), acceptingVerifier{userID: 17}),
	})

	conflict := performJSONRequest(router, stdhttp.MethodPost, "/api/v1/auth/register", `{"username":"alice","password":"password123"}`, nil)
	if conflict.Code != stdhttp.StatusConflict {
		t.Fatalf("register conflict status = %d", conflict.Code)
	}
	assertJSONEqual(t, conflict.Body.String(), `{"error":{"code":"username_conflict","message":"username is already in use"}}`)

	invalid := performJSONRequest(router, stdhttp.MethodPost, "/api/v1/auth/login", `{"username":"alice","password":"wrong-pass"}`, nil)
	if invalid.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("login invalid status = %d", invalid.Code)
	}
	assertJSONEqual(t, invalid.Body.String(), `{"error":{"code":"invalid_credentials","message":"invalid username or password"}}`)

	unknownField := performJSONRequest(router, stdhttp.MethodPost, "/api/v1/auth/register", `{"username":"alice","password":"password123","admin":true}`, nil)
	if unknownField.Code != stdhttp.StatusBadRequest {
		t.Fatalf("unknown field status = %d", unknownField.Code)
	}
	if !strings.Contains(unknownField.Body.String(), `"code":"validation_failed"`) {
		t.Fatalf("unknown field body = %s", unknownField.Body.String())
	}

	deleted := performJSONRequest(router, stdhttp.MethodGet, "/api/v1/users/me", "", &stdhttp.Cookie{Name: "session", Value: "signed-token"})
	if deleted.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("deleted user status = %d", deleted.Code)
	}
	assertSessionCookie(t, deleted, "session", "", -1)
}

func TestCurrentUserRequiresAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	application := &fakeAuthApplication{
		register: func(context.Context, auth.Credentials) (user.Public, string, error) { return user.Public{}, "", nil },
		login:    func(context.Context, auth.Credentials) (user.Public, string, error) { return user.Public{}, "", nil },
		currentUser: func(context.Context, uint64) (user.Public, error) {
			t.Fatal("CurrentUser() must not be called without authentication")
			return user.Public{}, nil
		},
	}
	cookies := auth.NewCookieManager("session", false, time.Hour, time.Now)
	router := NewRouter(Dependencies{}, APIRoutes{
		Auth:           auth.NewHandler(application, cookies),
		Authentication: middleware.RequireAuthentication(cookies.Name(), acceptingVerifier{userID: 17}),
	})

	response := performJSONRequest(router, stdhttp.MethodGet, "/api/v1/users/me", "", nil)
	if response.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
	assertJSONEqual(t, response.Body.String(), `{"error":{"code":"authentication_required","message":"authentication is required"}}`)
}

func performJSONRequest(handler stdhttp.Handler, method, path, body string, cookie *stdhttp.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertSessionCookie(t *testing.T, response *httptest.ResponseRecorder, name, value string, maxAge int) {
	t.Helper()
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookie count = %d, want 1; headers=%v", len(cookies), response.Header())
	}
	cookie := cookies[0]
	if cookie.Name != name || cookie.Value != value || cookie.MaxAge != maxAge {
		t.Fatalf("cookie = %#v, want name=%q value=%q MaxAge=%d", cookie, name, value, maxAge)
	}
	if !cookie.HttpOnly || cookie.SameSite != stdhttp.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" {
		t.Fatalf("cookie attributes = %#v", cookie)
	}
}

func assertNoSensitiveResponseData(t *testing.T, body string, values ...string) {
	t.Helper()
	for _, value := range values {
		if strings.Contains(body, value) {
			t.Fatalf("response leaked %q: %s", value, body)
		}
	}
}
