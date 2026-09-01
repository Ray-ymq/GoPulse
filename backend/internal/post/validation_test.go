package post

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeCreateInputTrimsAndCountsUnicodeCharacters(t *testing.T) {
	input := CreateInput{Title: " \u3000你好 GoPulse\t", Content: "\n正文内容🙂\u3000"}
	normalized, err := NormalizeCreateInput(input)
	if err != nil {
		t.Fatalf("NormalizeCreateInput() error = %v", err)
	}
	if normalized.Title != "你好 GoPulse" || normalized.Content != "正文内容🙂" {
		t.Fatalf("normalized input = %#v", normalized)
	}

	if _, err := NormalizeCreateInput(CreateInput{Title: strings.Repeat("界", 120), Content: strings.Repeat("🙂", 10000)}); err != nil {
		t.Fatalf("maximum Unicode boundaries rejected: %v", err)
	}
}

func TestNormalizeCreateInputRejectsWhitespaceInvalidUTF8AndLengthOverflow(t *testing.T) {
	tests := []struct {
		name  string
		input CreateInput
		want  error
	}{
		{name: "blank title", input: CreateInput{Title: " \u3000\t", Content: "content"}, want: ErrInvalidTitle},
		{name: "title too long", input: CreateInput{Title: strings.Repeat("a", 121), Content: "content"}, want: ErrInvalidTitle},
		{name: "invalid title utf8", input: CreateInput{Title: string([]byte{0xff}), Content: "content"}, want: ErrInvalidTitle},
		{name: "blank content", input: CreateInput{Title: "title", Content: "\n\t"}, want: ErrInvalidContent},
		{name: "content too long", input: CreateInput{Title: "title", Content: strings.Repeat("a", 10001)}, want: ErrInvalidContent},
		{name: "invalid content utf8", input: CreateInput{Title: "title", Content: string([]byte{0xff})}, want: ErrInvalidContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NormalizeCreateInput(test.input)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}
