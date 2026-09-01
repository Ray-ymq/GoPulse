package comment

import (
	"encoding/base64"
	"net/url"
	"testing"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
)

func TestCursorRoundTrip(t *testing.T) {
	token, err := EncodeCursor(Cursor{ID: ^uint64(0)})
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	if token == "" || token[len(token)-1] == '=' {
		t.Fatalf("token = %q, want non-empty unpadded token", token)
	}
	cursor, err := DecodeCursor(token)
	if err != nil || cursor.ID != ^uint64(0) {
		t.Fatalf("DecodeCursor() cursor=%#v error=%v", cursor, err)
	}
}

func TestDecodeCursorRejectsMalformedAndNonCanonicalPayloads(t *testing.T) {
	encode := func(payload string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(payload))
	}
	for _, token := range []string{
		"",
		"%%%",
		string(make([]byte, 257)),
		base64.URLEncoding.EncodeToString([]byte(`{"v":1,"id":1}`)),
		encode("not-json"),
		encode(`{"v":1}`),
		encode(`{"v":1,"id":0}`),
		encode(`{"v":2,"id":1}`),
		encode(`{"v":1,"id":1,"extra":true}`),
		encode(`{"v":1,"id":1}{}`),
	} {
		if _, err := DecodeCursor(token); err == nil {
			t.Fatalf("DecodeCursor(%q) error = nil", token)
		}
	}
}

func TestParseListOptionsDefaultsBoundariesAndFailures(t *testing.T) {
	options, err := ParseListOptions(url.Values{})
	if err != nil || options.Limit != DefaultLimit || options.Cursor != nil {
		t.Fatalf("default options=%#v error=%v", options, err)
	}
	token, err := EncodeCursor(Cursor{ID: 9})
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range []string{"1", "50"} {
		options, err = ParseListOptions(url.Values{"limit": {limit}, "cursor": {token}})
		if err != nil || options.Cursor == nil || options.Cursor.ID != 9 {
			t.Fatalf("limit %s options=%#v error=%v", limit, options, err)
		}
	}

	invalid := []url.Values{
		{"limit": {""}}, {"limit": {"0"}}, {"limit": {"51"}},
		{"limit": {"+1"}}, {"limit": {" 1"}}, {"limit": {"1", "2"}},
		{"cursor": {""}}, {"cursor": {"damaged"}}, {"cursor": {token, token}},
	}
	for _, values := range invalid {
		_, err := ParseListOptions(values)
		applicationError, ok := apperror.As(err)
		if !ok || applicationError.Code != apperror.CodeValidationFailed {
			t.Fatalf("ParseListOptions(%v) error=%#v, want validation_failed", values, err)
		}
	}
}
