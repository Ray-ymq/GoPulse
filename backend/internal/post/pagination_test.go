package post

import (
	"encoding/base64"
	"net/url"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
)

func TestCursorRoundTripUsesOpaqueURLSafeToken(t *testing.T) {
	want := Cursor{CreatedAt: time.Date(2026, 9, 1, 12, 34, 56, 123456000, time.FixedZone("offset", 8*60*60)), ID: 18446744073709551615}
	token, err := EncodeCursor(want)
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	if token == "" || token[len(token)-1] == '=' {
		t.Fatalf("token = %q, want non-empty unpadded token", token)
	}
	got, err := DecodeCursor(token)
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v", err)
	}
	if got.ID != want.ID || !got.CreatedAt.Equal(want.CreatedAt) || got.CreatedAt.Location() != time.UTC {
		t.Fatalf("decoded cursor = %#v, want ID/time %#v", got, want)
	}
}

func TestDecodeCursorRejectsMalformedAndNonCanonicalPayloads(t *testing.T) {
	encode := func(payload string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(payload))
	}
	tests := []struct {
		name  string
		token string
	}{
		{name: "empty", token: ""},
		{name: "not base64", token: "%%%"},
		{name: "ignored newline", token: encode(`{"v":1,"created_at":"2026-09-01T00:00:00Z","id":1}`) + "\n"},
		{name: "oversized", token: string(make([]byte, 513))},
		{name: "padded", token: base64.URLEncoding.EncodeToString([]byte(`{"v":1}`))},
		{name: "not json", token: encode("not-json")},
		{name: "missing fields", token: encode(`{"v":1}`)},
		{name: "unknown field", token: encode(`{"v":1,"created_at":"2026-09-01T00:00:00Z","id":1,"extra":true}`)},
		{name: "wrong version", token: encode(`{"v":2,"created_at":"2026-09-01T00:00:00Z","id":1}`)},
		{name: "zero id", token: encode(`{"v":1,"created_at":"2026-09-01T00:00:00Z","id":0}`)},
		{name: "offset timestamp", token: encode(`{"v":1,"created_at":"2026-09-01T08:00:00+08:00","id":1}`)},
		{name: "invalid timestamp", token: encode(`{"v":1,"created_at":"yesterday","id":1}`)},
		{name: "multiple values", token: encode(`{"v":1,"created_at":"2026-09-01T00:00:00Z","id":1}{}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeCursor(test.token); err == nil {
				t.Fatalf("DecodeCursor(%q) error = nil", test.token)
			}
		})
	}
}

func TestParseListOptionsDefaultsAndBoundaries(t *testing.T) {
	options, err := ParseListOptions(url.Values{})
	if err != nil || options.Limit != DefaultLimit || options.Cursor != nil {
		t.Fatalf("default options = %#v error=%v", options, err)
	}

	cursorToken, err := EncodeCursor(Cursor{CreatedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), ID: 9})
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range []string{"1", "50"} {
		options, err = ParseListOptions(url.Values{"limit": {limit}, "cursor": {cursorToken}})
		if err != nil || options.Cursor == nil {
			t.Fatalf("limit %s options=%#v error=%v", limit, options, err)
		}
	}
}

func TestParseListOptionsRejectsInvalidOrDuplicateValues(t *testing.T) {
	tests := []url.Values{
		{"limit": {""}}, {"limit": {"0"}}, {"limit": {"51"}},
		{"limit": {"+1"}}, {"limit": {" 1"}}, {"limit": {"1", "2"}},
		{"cursor": {""}}, {"cursor": {"invalid"}}, {"cursor": {"one", "two"}},
	}
	for _, values := range tests {
		_, err := ParseListOptions(values)
		appError, ok := apperror.As(err)
		if !ok || appError.Code != apperror.CodeValidationFailed {
			t.Fatalf("ParseListOptions(%v) error = %#v, want validation_failed", values, err)
		}
	}
}
