package user

import (
	"strings"
	"unicode/utf8"

	"github.com/Ray-ymq/GoPulse/backend/internal/shared/apperror"
)

func NormalizeProfile(input ProfileInput) (string, string, error) {
	if input.DisplayName == nil || input.Bio == nil {
		return "", "", invalid("display_name and bio are required")
	}
	name, bio := strings.TrimSpace(*input.DisplayName), strings.TrimSpace(*input.Bio)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 64 {
		return "", "", invalid("display_name must contain 1–64 characters")
	}
	if !utf8.ValidString(bio) || utf8.RuneCountInString(bio) > 160 {
		return "", "", invalid("bio must contain 0–160 characters")
	}
	return name, bio, nil
}

func invalid(message string) error { return apperror.New(apperror.CodeValidationFailed, message) }
