package auth

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// PasswordManager owns the project's bcrypt cost and the constant-work path
// used when a login username does not exist.
type PasswordManager struct {
	cost      int
	dummyHash []byte
}

func NewPasswordManager() (*PasswordManager, error) {
	dummyHash, err := bcrypt.GenerateFromPassword([]byte("gopulse-nonexistent-user-password"), bcrypt.DefaultCost)
	if err != nil {
		return nil, errors.New("initialize password manager")
	}
	return &PasswordManager{cost: bcrypt.DefaultCost, dummyHash: dummyHash}, nil
}

func (manager *PasswordManager) Hash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), manager.cost)
	if err != nil {
		return "", errors.New("hash password")
	}
	return string(hash), nil
}

func (manager *PasswordManager) Verify(passwordHash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)) == nil
}

func (manager *PasswordManager) VerifyUnknown(password string) {
	_ = bcrypt.CompareHashAndPassword(manager.dummyHash, []byte(password))
}
