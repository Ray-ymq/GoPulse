package auth

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTokenManagerIssuesAndVerifiesHS256Token(t *testing.T) {
	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	manager := mustTokenManager(t, now)

	token, err := manager.Issue(42)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if strings.Contains(token, "42") {
		t.Fatal("token unexpectedly contains the plain subject")
	}
	userID, err := manager.Verify(token)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if userID != 42 {
		t.Fatalf("Verify() user ID = %d, want 42", userID)
	}
}

func TestTokenManagerRejectsExpiredTamperedAndMalformedTokens(t *testing.T) {
	baseTime := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	current := baseTime
	manager, err := NewTokenManager("test-jwt-secret-at-least-32-bytes-long", 2*time.Hour, func() time.Time { return current })
	if err != nil {
		t.Fatalf("NewTokenManager() error = %v", err)
	}
	valid, err := manager.Issue(7)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	current = baseTime.Add(2 * time.Hour)
	assertInvalidToken(t, manager, valid)
	current = baseTime

	segments := strings.Split(valid, ".")
	tamperedPayload := encodeSegment([]byte(`{"sub":"8","iat":1788264000,"exp":1788271200}`))
	assertInvalidToken(t, manager, segments[0]+"."+tamperedPayload+"."+segments[2])
	assertInvalidToken(t, manager, "not-a-jwt")
	assertInvalidToken(t, manager, strings.Repeat("a", maximumTokenBytes+1))
}

func TestTokenManagerRejectsAlgorithmSubstitutionMissingClaimsAndInvalidSubject(t *testing.T) {
	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	manager := mustTokenManager(t, now)
	issued := now.Unix()
	expires := now.Add(time.Hour).Unix()

	tests := []struct {
		name   string
		header map[string]any
		claims map[string]any
	}{
		{name: "none algorithm", header: map[string]any{"alg": "none", "typ": "JWT"}, claims: validClaims("1", issued, expires)},
		{name: "algorithm substitution", header: map[string]any{"alg": "HS512", "typ": "JWT"}, claims: validClaims("1", issued, expires)},
		{name: "missing subject", header: validHeader(), claims: map[string]any{"iat": issued, "exp": expires}},
		{name: "missing issued at", header: validHeader(), claims: map[string]any{"sub": "1", "exp": expires}},
		{name: "missing expiration", header: validHeader(), claims: map[string]any{"sub": "1", "iat": issued}},
		{name: "zero subject", header: validHeader(), claims: validClaims("0", issued, expires)},
		{name: "negative subject", header: validHeader(), claims: validClaims("-1", issued, expires)},
		{name: "non decimal subject", header: validHeader(), claims: validClaims("user-1", issued, expires)},
		{name: "future issued at", header: validHeader(), claims: validClaims("1", issued+1, expires)},
		{name: "expiration before issued at", header: validHeader(), claims: validClaims("1", issued, issued)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertInvalidToken(t, manager, signedToken(t, manager, test.header, test.claims))
		})
	}
}

func mustTokenManager(t *testing.T, now time.Time) *TokenManager {
	t.Helper()
	manager, err := NewTokenManager("test-jwt-secret-at-least-32-bytes-long", 2*time.Hour, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewTokenManager() error = %v", err)
	}
	return manager
}

func signedToken(t *testing.T, manager *TokenManager, header, claims map[string]any) string {
	t.Helper()
	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("json.Marshal(header) error = %v", err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("json.Marshal(claims) error = %v", err)
	}
	unsigned := encodeSegment(headerJSON) + "." + encodeSegment(claimsJSON)
	return unsigned + "." + encodeSegment(manager.sign(unsigned))
}

func validHeader() map[string]any {
	return map[string]any{"alg": "HS256", "typ": "JWT"}
}

func validClaims(subject string, issuedAt, expires int64) map[string]any {
	return map[string]any{"sub": subject, "iat": issuedAt, "exp": expires}
}

func assertInvalidToken(t *testing.T, manager *TokenManager, token string) {
	t.Helper()
	if _, err := manager.Verify(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Verify() error = %v, want ErrInvalidToken", err)
	}
}
