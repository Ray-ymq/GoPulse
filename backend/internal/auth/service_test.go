package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
)

type fakeUserRepository struct {
	create         func(context.Context, string, string) (user.User, error)
	findByID       func(context.Context, uint64) (user.User, error)
	findByUsername func(context.Context, string) (user.User, error)
}

func (repository *fakeUserRepository) Create(ctx context.Context, username, passwordHash string) (user.User, error) {
	if repository.create == nil {
		return user.User{}, errors.New("unexpected Create call")
	}
	return repository.create(ctx, username, passwordHash)
}

func (repository *fakeUserRepository) FindByID(ctx context.Context, identifier uint64) (user.User, error) {
	if repository.findByID == nil {
		return user.User{}, errors.New("unexpected FindByID call")
	}
	return repository.findByID(ctx, identifier)
}

func (repository *fakeUserRepository) FindByUsername(ctx context.Context, username string) (user.User, error) {
	if repository.findByUsername == nil {
		return user.User{}, errors.New("unexpected FindByUsername call")
	}
	return repository.findByUsername(ctx, username)
}

type fakePasswordOperations struct {
	hashResult         string
	hashError          error
	verifyResult       bool
	verifyUnknownCalls int
}

func (operations *fakePasswordOperations) Hash(string) (string, error) {
	return operations.hashResult, operations.hashError
}

func (operations *fakePasswordOperations) Verify(string, string) bool {
	return operations.verifyResult
}

func (operations *fakePasswordOperations) VerifyUnknown(string) {
	operations.verifyUnknownCalls++
}

type fakeTokenIssuer struct {
	token string
	err   error
	ids   []uint64
}

func (issuer *fakeTokenIssuer) Issue(identifier uint64) (string, error) {
	issuer.ids = append(issuer.ids, identifier)
	return issuer.token, issuer.err
}

func TestServiceRegisterNormalizesUsernameBeforeCreatingUser(t *testing.T) {
	createdAt := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	passwords := &fakePasswordOperations{hashResult: "$2a$10$hash"}
	tokens := &fakeTokenIssuer{token: "signed-token"}
	repository := &fakeUserRepository{create: func(_ context.Context, username, passwordHash string) (user.User, error) {
		if username != "Alice_01" {
			t.Fatalf("Create() username = %q, want Alice_01", username)
		}
		if passwordHash != passwords.hashResult {
			t.Fatalf("Create() hash = %q, want generated hash", passwordHash)
		}
		return user.User{ID: 9, Username: username, PasswordHash: passwordHash, Role: user.RoleUser, CreatedAt: createdAt}, nil
	}}
	service := NewService(repository, passwords, tokens)

	publicUser, token, err := service.Register(context.Background(), Credentials{Username: "  Alice_01\t", Password: "password123"})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if publicUser.ID != 9 || publicUser.Username != "Alice_01" || !publicUser.CreatedAt.Equal(createdAt) {
		t.Fatalf("Register() user = %#v", publicUser)
	}
	if token != "signed-token" || len(tokens.ids) != 1 || tokens.ids[0] != 9 {
		t.Fatalf("Register() token result = %q ids=%v", token, tokens.ids)
	}
}

func TestServiceRegisterDoesNotCreateUserWhenHashingFails(t *testing.T) {
	createCalled := false
	repository := &fakeUserRepository{create: func(context.Context, string, string) (user.User, error) {
		createCalled = true
		return user.User{}, nil
	}}
	service := NewService(repository, &fakePasswordOperations{hashError: errors.New("bcrypt failed")}, &fakeTokenIssuer{})

	_, _, err := service.Register(context.Background(), Credentials{Username: "alice", Password: "password123"})
	assertApplicationCode(t, err, apperror.CodeInternal)
	if createCalled {
		t.Fatal("Create() was called after password hashing failed")
	}
}

func TestServiceRegisterMapsValidationAndUniqueConstraintErrors(t *testing.T) {
	service := NewService(
		&fakeUserRepository{create: func(context.Context, string, string) (user.User, error) {
			return user.User{}, user.ErrUsernameConflict
		}},
		&fakePasswordOperations{hashResult: "hash"},
		&fakeTokenIssuer{},
	)

	_, _, err := service.Register(context.Background(), Credentials{Username: "ab", Password: "password123"})
	assertApplicationCode(t, err, apperror.CodeValidationFailed)
	_, _, err = service.Register(context.Background(), Credentials{Username: "alice", Password: "short"})
	assertApplicationCode(t, err, apperror.CodeValidationFailed)
	_, _, err = service.Register(context.Background(), Credentials{Username: "alice", Password: "password123"})
	assertApplicationCode(t, err, apperror.CodeUsernameConflict)
}

func TestServiceLoginUsesIndistinguishableErrorsForUnknownUserAndWrongPassword(t *testing.T) {
	for _, test := range []struct {
		name       string
		repository *fakeUserRepository
		passwords  *fakePasswordOperations
	}{
		{
			name: "unknown user",
			repository: &fakeUserRepository{findByUsername: func(context.Context, string) (user.User, error) {
				return user.User{}, user.ErrNotFound
			}},
			passwords: &fakePasswordOperations{},
		},
		{
			name: "wrong password",
			repository: &fakeUserRepository{findByUsername: func(context.Context, string) (user.User, error) {
				return user.User{ID: 1, Username: "alice", PasswordHash: "hash", Role: user.RoleUser}, nil
			}},
			passwords: &fakePasswordOperations{verifyResult: false},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(test.repository, test.passwords, &fakeTokenIssuer{})
			_, _, err := service.Login(context.Background(), Credentials{Username: "alice", Password: "password123"})
			appError := assertApplicationCode(t, err, apperror.CodeInvalidCredentials)
			if appError.Message != "invalid username or password" {
				t.Fatalf("message = %q, want indistinguishable credential message", appError.Message)
			}
		})
	}
}

func TestServiceLoginIssuesTokenForValidCredentials(t *testing.T) {
	passwords := &fakePasswordOperations{verifyResult: true}
	tokens := &fakeTokenIssuer{token: "signed-token"}
	service := NewService(&fakeUserRepository{findByUsername: func(_ context.Context, username string) (user.User, error) {
		return user.User{ID: 15, Username: username, PasswordHash: "hash", Role: user.RoleAdmin, CreatedAt: time.Now()}, nil
	}}, passwords, tokens)

	publicUser, token, err := service.Login(context.Background(), Credentials{Username: " Alice ", Password: "password123"})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if publicUser.ID != 15 || publicUser.Username != "Alice" || token != "signed-token" {
		t.Fatalf("Login() result user=%#v token=%q", publicUser, token)
	}
}

func TestServiceCurrentUserMapsDeletedUserToAuthenticationRequired(t *testing.T) {
	service := NewService(&fakeUserRepository{findByID: func(context.Context, uint64) (user.User, error) {
		return user.User{}, user.ErrNotFound
	}}, &fakePasswordOperations{}, &fakeTokenIssuer{})

	_, err := service.CurrentUser(context.Background(), 99)
	assertApplicationCode(t, err, apperror.CodeAuthenticationRequired)
}

func assertApplicationCode(t *testing.T, err error, code apperror.Code) *apperror.Error {
	t.Helper()
	appError, ok := apperror.As(err)
	if !ok {
		t.Fatalf("error = %v, want application error", err)
	}
	if appError.Code != code {
		t.Fatalf("error code = %q, want %q", appError.Code, code)
	}
	return appError
}
