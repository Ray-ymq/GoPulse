package user

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var (
	ErrInvalidUsername = errors.New("invalid username")
	ErrInvalidPassword = errors.New("invalid password")
)

// NormalizeUsername removes surrounding whitespace and validates the stable
// Phase 1 ASCII username contract.
func NormalizeUsername(value string) (string, error) {
	normalized := strings.TrimSpace(value)
	if len(normalized) < 3 || len(normalized) > 32 {
		return "", ErrInvalidUsername
	}
	for index := 0; index < len(normalized); index++ {
		character := normalized[index]
		if (character >= 'A' && character <= 'Z') ||
			(character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '_' {
			continue
		}
		return "", ErrInvalidUsername
	}
	return normalized, nil
}

// ValidatePassword keeps password bytes unchanged. It requires at least eight
// Unicode characters and enforces bcrypt's 72-byte input limit.
func ValidatePassword(value string) error {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 8 || len([]byte(value)) > 72 {
		return ErrInvalidPassword
	}
	return nil
}
