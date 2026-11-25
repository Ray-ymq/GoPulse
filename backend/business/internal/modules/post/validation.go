package post

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var (
	ErrInvalidTitle   = errors.New("invalid post title")
	ErrInvalidContent = errors.New("invalid post content")
)

// NormalizeCreateInput trims surrounding Unicode whitespace and validates
// character-count limits that match the MySQL utf8mb4 schema contract.
func NormalizeCreateInput(input CreateInput) (CreateInput, error) {
	title := strings.TrimSpace(input.Title)
	if !utf8.ValidString(title) || utf8.RuneCountInString(title) < 1 || utf8.RuneCountInString(title) > 120 {
		return CreateInput{}, ErrInvalidTitle
	}

	content := strings.TrimSpace(input.Content)
	if !utf8.ValidString(content) || utf8.RuneCountInString(content) < 1 || utf8.RuneCountInString(content) > 10000 {
		return CreateInput{}, ErrInvalidContent
	}

	return CreateInput{Title: title, Content: content}, nil
}
