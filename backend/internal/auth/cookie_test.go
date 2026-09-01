package auth

import (
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCookieManagerSetsSecureHttpOnlySessionCookie(t *testing.T) {
	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	manager := NewCookieManager("session", true, 2*time.Hour, func() time.Time { return now })
	response := httptest.NewRecorder()

	manager.Set(response, "signed-token")
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookie count = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != "session" || cookie.Value != "signed-token" {
		t.Fatalf("cookie identity = %#v", cookie)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != stdhttp.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatalf("cookie security attributes = %#v", cookie)
	}
	if cookie.Domain != "" {
		t.Fatalf("cookie Domain = %q, want empty", cookie.Domain)
	}
	if cookie.MaxAge != 7200 || !cookie.Expires.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("cookie lifetime = MaxAge %d Expires %s", cookie.MaxAge, cookie.Expires)
	}
}

func TestCookieManagerClearsCookieWithMatchingScope(t *testing.T) {
	manager := NewCookieManager("session", false, 2*time.Hour, time.Now)
	response := httptest.NewRecorder()

	manager.Clear(response)
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookie count = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != "session" || cookie.Value != "" || cookie.Path != "/" || cookie.MaxAge != -1 {
		t.Fatalf("cleared cookie = %#v", cookie)
	}
	if !cookie.HttpOnly || cookie.Secure || cookie.SameSite != stdhttp.SameSiteLaxMode {
		t.Fatalf("cleared cookie security attributes = %#v", cookie)
	}
	if cookie.Expires.After(time.Unix(2, 0)) {
		t.Fatalf("cleared cookie Expires = %s, want expired", cookie.Expires)
	}
}
