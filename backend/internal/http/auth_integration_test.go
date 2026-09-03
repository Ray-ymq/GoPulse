//go:build integration

package http

import (
	"context"
	"database/sql"
	stdhttp "net/http"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/auth"
	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
	"github.com/gin-gonic/gin"
)

func TestIntegrationAuthenticationHTTPFlowAndApplicationRestart(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := integrationtest.Environment(t)
	username := "HttpIT_" + time.Now().UTC().Format("150405000000")
	password := "integration-password"
	cleanupHTTPIntegrationUser(t, cfg, username)

	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("OpenMySQLDatabase() error = %v", err)
	}
	router := integrationAuthRouter(t, cfg, database)

	register := performJSONRequest(router, stdhttp.MethodPost, "/api/v1/auth/register", `{"username":"`+username+`","password":"`+password+`"}`, nil)
	if register.Code != stdhttp.StatusCreated {
		_ = database.Close()
		t.Fatalf("register status = %d body=%s", register.Code, register.Body.String())
	}
	cookies := register.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].Value == "" {
		_ = database.Close()
		t.Fatalf("register cookie = %#v", cookies)
	}

	var storedHash string
	if err := database.QueryRow(`SELECT password_hash FROM users WHERE username = ?`, username).Scan(&storedHash); err != nil {
		_ = database.Close()
		t.Fatalf("query stored hash: %v", err)
	}
	if storedHash == password || !strings.HasPrefix(storedHash, "$2") {
		_ = database.Close()
		t.Fatal("registered password was not stored as bcrypt")
	}

	duplicate := performJSONRequest(router, stdhttp.MethodPost, "/api/v1/auth/register", `{"username":"`+strings.ToLower(username)+`","password":"`+password+`"}`, nil)
	if duplicate.Code != stdhttp.StatusConflict {
		_ = database.Close()
		t.Fatalf("case-insensitive duplicate status = %d body=%s", duplicate.Code, duplicate.Body.String())
	}

	current := performJSONRequest(router, stdhttp.MethodGet, "/api/v1/users/me", "", cookies[0])
	if current.Code != stdhttp.StatusOK || !strings.Contains(current.Body.String(), username) || !strings.Contains(current.Body.String(), `"role":"user"`) {
		_ = database.Close()
		t.Fatalf("current user status=%d body=%s", current.Code, current.Body.String())
	}
	if _, err := user.NewMySQLRepository(database).PromoteByUsername(context.Background(), username); err != nil {
		_ = database.Close()
		t.Fatalf("promote current user: %v", err)
	}
	promotedCurrent := performJSONRequest(router, stdhttp.MethodGet, "/api/v1/users/me", "", cookies[0])
	if promotedCurrent.Code != stdhttp.StatusOK || !strings.Contains(promotedCurrent.Body.String(), `"role":"admin"`) {
		_ = database.Close()
		t.Fatalf("current user after promotion status=%d body=%s", promotedCurrent.Code, promotedCurrent.Body.String())
	}

	logout := performJSONRequest(router, stdhttp.MethodPost, "/api/v1/auth/logout", "", nil)
	if logout.Code != stdhttp.StatusNoContent || len(logout.Result().Cookies()) != 1 || logout.Result().Cookies()[0].MaxAge != -1 {
		_ = database.Close()
		t.Fatalf("logout status=%d cookies=%#v", logout.Code, logout.Result().Cookies())
	}
	unauthenticated := performJSONRequest(router, stdhttp.MethodGet, "/api/v1/users/me", "", nil)
	if unauthenticated.Code != stdhttp.StatusUnauthorized {
		_ = database.Close()
		t.Fatalf("current user after logout status = %d", unauthenticated.Code)
	}

	wrongPassword := performJSONRequest(router, stdhttp.MethodPost, "/api/v1/auth/login", `{"username":"`+username+`","password":"incorrect-password"}`, nil)
	if wrongPassword.Code != stdhttp.StatusUnauthorized {
		_ = database.Close()
		t.Fatalf("wrong-password login status = %d", wrongPassword.Code)
	}

	if err := database.Close(); err != nil {
		t.Fatalf("close application database pool: %v", err)
	}
	restartedDatabase, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("reopen database for application restart: %v", err)
	}
	defer restartedDatabase.Close()
	restartedRouter := integrationAuthRouter(t, cfg, restartedDatabase)
	login := performJSONRequest(restartedRouter, stdhttp.MethodPost, "/api/v1/auth/login", `{"username":"`+strings.ToLower(username)+`","password":"`+password+`"}`, nil)
	if login.Code != stdhttp.StatusOK || len(login.Result().Cookies()) != 1 || login.Result().Cookies()[0].Value == "" || !strings.Contains(login.Body.String(), `"role":"admin"`) {
		t.Fatalf("login after application restart status=%d body=%s cookies=%#v", login.Code, login.Body.String(), login.Result().Cookies())
	}
}

func integrationAuthRouter(t *testing.T, cfg config.Config, database *sql.DB) stdhttp.Handler {
	t.Helper()
	passwords, err := auth.NewPasswordManager()
	if err != nil {
		t.Fatalf("NewPasswordManager() error = %v", err)
	}
	tokens, err := auth.NewTokenManager(cfg.Auth.JWTSecret, cfg.Auth.JWTTTL, time.Now)
	if err != nil {
		t.Fatalf("NewTokenManager() error = %v", err)
	}
	cookies := auth.NewCookieManager(cfg.Auth.CookieName, cfg.Auth.CookieSecure, cfg.Auth.JWTTTL, time.Now)
	service := auth.NewService(user.NewMySQLRepository(database), passwords, tokens)
	return NewRouter(Dependencies{}, APIRoutes{
		Auth:           auth.NewHandler(service, cookies),
		Authentication: middleware.RequireAuthentication(cookies.Name(), tokens),
	})
}

func cleanupHTTPIntegrationUser(t *testing.T, cfg config.Config, username string) {
	t.Helper()
	t.Cleanup(func() {
		database, err := platform.OpenMySQLDatabase(cfg.MySQL)
		if err != nil {
			t.Errorf("cleanup OpenMySQLDatabase() error = %v", err)
			return
		}
		defer database.Close()
		if _, err := database.ExecContext(context.Background(), `DELETE FROM users WHERE username = ?`, username); err != nil {
			t.Errorf("cleanup HTTP integration user: %v", err)
		}
	})
}
