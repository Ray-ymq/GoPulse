//go:build integration

package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
)

func TestIntegrationRegistrationCaseInsensitiveConflictAndRestartLogin(t *testing.T) {
	cfg := integrationtest.Environment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	username := "AuthIT_" + time.Now().UTC().Format("150405000000")
	password := "integration-password"
	cleanupIntegrationUser(t, cfg, username)

	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("OpenMySQLDatabase() error = %v", err)
	}
	passwords, err := NewPasswordManager()
	if err != nil {
		t.Fatalf("NewPasswordManager() error = %v", err)
	}
	tokens, err := NewTokenManager(cfg.Auth.JWTSecret, cfg.Auth.JWTTTL, time.Now)
	if err != nil {
		t.Fatalf("NewTokenManager() error = %v", err)
	}
	service := NewService(user.NewMySQLRepository(database), passwords, tokens)

	registered, token, err := service.Register(ctx, Credentials{Username: username, Password: password})
	if err != nil {
		_ = database.Close()
		t.Fatalf("Register() error = %v", err)
	}
	if registered.ID == 0 || registered.Username != username || token == "" {
		_ = database.Close()
		t.Fatalf("Register() result user=%#v tokenEmpty=%v", registered, token == "")
	}

	var storedHash string
	if err := database.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, registered.ID).Scan(&storedHash); err != nil {
		_ = database.Close()
		t.Fatalf("query stored password hash: %v", err)
	}
	if storedHash == password || !strings.HasPrefix(storedHash, "$2") || !passwords.Verify(storedHash, password) {
		_ = database.Close()
		t.Fatal("MySQL did not persist a valid bcrypt hash")
	}

	_, _, err = service.Register(ctx, Credentials{Username: strings.ToLower(username), Password: password})
	assertIntegrationCode(t, err, apperror.CodeUsernameConflict)

	if err := database.Close(); err != nil {
		t.Fatalf("close first application database pool: %v", err)
	}

	restartedDatabase, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("reopen MySQL database after simulated Backend restart: %v", err)
	}
	defer restartedDatabase.Close()
	restartedService := NewService(user.NewMySQLRepository(restartedDatabase), passwords, tokens)
	loggedIn, restartedToken, err := restartedService.Login(ctx, Credentials{Username: strings.ToLower(username), Password: password})
	if err != nil {
		t.Fatalf("Login() after database pool restart error = %v", err)
	}
	if loggedIn.ID != registered.ID || restartedToken == "" {
		t.Fatalf("Login() after restart user=%#v tokenEmpty=%v", loggedIn, restartedToken == "")
	}

	_, _, err = restartedService.Login(ctx, Credentials{Username: username, Password: "incorrect-password"})
	assertIntegrationCode(t, err, apperror.CodeInvalidCredentials)
}

func cleanupIntegrationUser(t *testing.T, cfg config.Config, username string) {
	t.Helper()
	t.Cleanup(func() {
		database, err := platform.OpenMySQLDatabase(cfg.MySQL)
		if err != nil {
			t.Errorf("cleanup OpenMySQLDatabase() error = %v", err)
			return
		}
		defer database.Close()
		if _, err := database.Exec(`DELETE FROM users WHERE username = ?`, username); err != nil {
			t.Errorf("cleanup integration user: %v", err)
		}
	})
}

func assertIntegrationCode(t *testing.T, err error, want apperror.Code) {
	t.Helper()
	appError, ok := apperror.As(err)
	if !ok || appError.Code != want {
		t.Fatalf("error = %v, want application code %q", err, want)
	}
}
