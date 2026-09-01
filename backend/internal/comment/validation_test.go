package comment

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeCreateInputTrimsAndCountsUnicodeCharacters(t *testing.T) {
	normalized, err := NormalizeCreateInput(CreateInput{Content: " \n评论内容🙂\u3000"})
	if err != nil {
		t.Fatalf("NormalizeCreateInput() error = %v", err)
	}
	if normalized.Content != "评论内容🙂" {
		t.Fatalf("normalized content = %q", normalized.Content)
	}
	if _, err := NormalizeCreateInput(CreateInput{Content: strings.Repeat("界", 2000)}); err != nil {
		t.Fatalf("maximum Unicode boundary rejected: %v", err)
	}
}

func TestNormalizeCreateInputRejectsBlankInvalidUTF8AndOverflow(t *testing.T) {
	for _, input := range []CreateInput{
		{Content: " \n\t\u3000"},
		{Content: strings.Repeat("a", 2001)},
		{Content: string([]byte{0xff})},
	} {
		if _, err := NormalizeCreateInput(input); !errors.Is(err, ErrInvalidContent) {
			t.Fatalf("NormalizeCreateInput(%q) error = %v, want ErrInvalidContent", input.Content, err)
		}
	}
}
