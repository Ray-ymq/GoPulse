package user

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeUsername(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		err   error
	}{
		{name: "minimum", input: "abc", want: "abc"},
		{name: "maximum", input: strings.Repeat("a", 32), want: strings.Repeat("a", 32)},
		{name: "surrounding whitespace", input: " \tAlice_01\n", want: "Alice_01"},
		{name: "case preserved", input: "Alice", want: "Alice"},
		{name: "too short", input: "ab", err: ErrInvalidUsername},
		{name: "too long", input: strings.Repeat("a", 33), err: ErrInvalidUsername},
		{name: "internal whitespace", input: "ali ce", err: ErrInvalidUsername},
		{name: "punctuation", input: "alice!", err: ErrInvalidUsername},
		{name: "unicode", input: "爱丽丝", err: ErrInvalidUsername},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := NormalizeUsername(test.input)
			if !errors.Is(err, test.err) {
				t.Fatalf("NormalizeUsername() error = %v, want %v", err, test.err)
			}
			if actual != test.want {
				t.Fatalf("NormalizeUsername() = %q, want %q", actual, test.want)
			}
		})
	}
}

func TestValidatePasswordCharacterAndByteBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		password string
		valid    bool
	}{
		{name: "eight ASCII characters", password: "12345678", valid: true},
		{name: "seventy two bytes", password: strings.Repeat("a", 72), valid: true},
		{name: "seven characters", password: "1234567", valid: false},
		{name: "unicode within byte limit", password: strings.Repeat("界", 8), valid: true},
		{name: "unicode over byte limit", password: strings.Repeat("界", 25), valid: false},
		{name: "whitespace preserved and counted", password: " 123456 ", valid: true},
		{name: "invalid UTF-8", password: string([]byte{0xff, '1', '2', '3', '4', '5', '6', '7'}), valid: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidatePassword(test.password)
			if test.valid && err != nil {
				t.Fatalf("ValidatePassword() error = %v", err)
			}
			if !test.valid && !errors.Is(err, ErrInvalidPassword) {
				t.Fatalf("ValidatePassword() error = %v, want ErrInvalidPassword", err)
			}
		})
	}
}
