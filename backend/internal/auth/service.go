package auth

import (
	"context"
	"errors"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
)

type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type passwordOperations interface {
	Hash(string) (string, error)
	Verify(string, string) bool
	VerifyUnknown(string)
}

type tokenIssuer interface {
	Issue(uint64) (string, error)
}

type Service struct {
	users     user.Repository
	passwords passwordOperations
	tokens    tokenIssuer
}

func NewService(users user.Repository, passwords passwordOperations, tokens tokenIssuer) *Service {
	return &Service{users: users, passwords: passwords, tokens: tokens}
}

func (service *Service) Register(ctx context.Context, credentials Credentials) (user.Public, string, error) {
	username, err := user.NormalizeUsername(credentials.Username)
	if err != nil {
		return user.Public{}, "", apperror.New(apperror.CodeValidationFailed, "username must match [A-Za-z0-9_]{3,32}")
	}
	if err := user.ValidatePassword(credentials.Password); err != nil {
		return user.Public{}, "", apperror.New(apperror.CodeValidationFailed, "password must contain at least 8 characters and at most 72 bytes")
	}

	passwordHash, err := service.passwords.Hash(credentials.Password)
	if err != nil {
		return user.Public{}, "", apperror.WrapInternal(err)
	}
	record, err := service.users.Create(ctx, username, passwordHash)
	if errors.Is(err, user.ErrUsernameConflict) {
		return user.Public{}, "", apperror.New(apperror.CodeUsernameConflict, "username is already in use")
	}
	if err != nil {
		return user.Public{}, "", apperror.WrapInternal(err)
	}

	token, err := service.tokens.Issue(record.ID)
	if err != nil {
		return user.Public{}, "", apperror.WrapInternal(err)
	}
	return record.Public(), token, nil
}

func (service *Service) Login(ctx context.Context, credentials Credentials) (user.Public, string, error) {
	username, validationErr := user.NormalizeUsername(credentials.Username)
	passwordErr := user.ValidatePassword(credentials.Password)
	if validationErr != nil || passwordErr != nil {
		service.passwords.VerifyUnknown(credentials.Password)
		return user.Public{}, "", invalidCredentialsError()
	}

	record, err := service.users.FindByUsername(ctx, username)
	if errors.Is(err, user.ErrNotFound) {
		service.passwords.VerifyUnknown(credentials.Password)
		return user.Public{}, "", invalidCredentialsError()
	}
	if err != nil {
		return user.Public{}, "", apperror.WrapInternal(err)
	}
	if !service.passwords.Verify(record.PasswordHash, credentials.Password) {
		return user.Public{}, "", invalidCredentialsError()
	}

	token, err := service.tokens.Issue(record.ID)
	if err != nil {
		return user.Public{}, "", apperror.WrapInternal(err)
	}
	return record.Public(), token, nil
}

func (service *Service) CurrentUser(ctx context.Context, userID uint64) (user.Public, error) {
	record, err := service.users.FindByID(ctx, userID)
	if errors.Is(err, user.ErrNotFound) {
		return user.Public{}, authenticationRequiredError()
	}
	if err != nil {
		return user.Public{}, apperror.WrapInternal(err)
	}
	return record.Public(), nil
}

func invalidCredentialsError() error {
	return apperror.New(apperror.CodeInvalidCredentials, "invalid username or password")
}

func authenticationRequiredError() error {
	return apperror.New(apperror.CodeAuthenticationRequired, "authentication is required")
}
