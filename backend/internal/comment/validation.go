package comment

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrInvalidContent = errors.New("invalid comment content")

// NormalizeCreateInput trims surrounding Unicode whitespace and validates the
// character-count limit that matches the MySQL utf8mb4 schema contract.
func NormalizeCreateInput(input CreateInput) (CreateInput, error) {
	content := strings.TrimSpace(input.Content)
	if !utf8.ValidString(content) || utf8.RuneCountInString(content) < 1 || utf8.RuneCountInString(content) > 2000 {
		return CreateInput{}, ErrInvalidContent
	}
	return CreateInput{Content: content}, nil
}
