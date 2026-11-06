package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var ErrInvalidToken = errors.New("invalid authentication token")

const maximumTokenBytes = 4096

type clockFunc func() time.Time

type TokenManager struct {
	secret []byte
	ttl    time.Duration
	now    clockFunc
}

type tokenHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}

type tokenClaims struct {
	Subject  string `json:"sub"`
	IssuedAt int64  `json:"iat"`
	Expires  int64  `json:"exp"`
}

func NewTokenManager(secret string, ttl time.Duration, now func() time.Time) (*TokenManager, error) {
	if secret == "" {
		return nil, errors.New("JWT secret is required")
	}
	if ttl <= 0 {
		return nil, errors.New("JWT TTL must be positive")
	}
	if now == nil {
		now = time.Now
	}
	return &TokenManager{secret: []byte(secret), ttl: ttl, now: now}, nil
}

func (manager *TokenManager) Issue(userID uint64) (string, error) {
	if userID == 0 {
		return "", errors.New("user ID must be positive")
	}

	issuedAt := manager.now().UTC().Truncate(time.Second)
	headerBytes, err := json.Marshal(tokenHeader{Algorithm: "HS256", Type: "JWT"})
	if err != nil {
		return "", errors.New("encode JWT header")
	}
	claimsBytes, err := json.Marshal(tokenClaims{
		Subject:  strconv.FormatUint(userID, 10),
		IssuedAt: issuedAt.Unix(),
		Expires:  issuedAt.Add(manager.ttl).Unix(),
	})
	if err != nil {
		return "", errors.New("encode JWT claims")
	}

	unsigned := encodeSegment(headerBytes) + "." + encodeSegment(claimsBytes)
	return unsigned + "." + encodeSegment(manager.sign(unsigned)), nil
}

func (manager *TokenManager) Verify(token string) (uint64, error) {
	if token == "" || len(token) > maximumTokenBytes {
		return 0, ErrInvalidToken
	}
	segments := strings.Split(token, ".")
	if len(segments) != 3 || segments[0] == "" || segments[1] == "" || segments[2] == "" {
		return 0, ErrInvalidToken
	}

	headerBytes, err := decodeSegment(segments[0])
	if err != nil {
		return 0, ErrInvalidToken
	}
	var header tokenHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil || header.Algorithm != "HS256" || header.Type != "JWT" {
		return 0, ErrInvalidToken
	}

	signature, err := decodeSegment(segments[2])
	if err != nil || !hmac.Equal(signature, manager.sign(segments[0]+"."+segments[1])) {
		return 0, ErrInvalidToken
	}

	claimsBytes, err := decodeSegment(segments[1])
	if err != nil {
		return 0, ErrInvalidToken
	}
	claims, err := decodeClaims(claimsBytes)
	if err != nil {
		return 0, ErrInvalidToken
	}

	userID, err := strconv.ParseUint(claims.Subject, 10, 64)
	if err != nil || userID == 0 {
		return 0, ErrInvalidToken
	}

	nowUnix := manager.now().UTC().Unix()
	if claims.IssuedAt <= 0 || claims.Expires <= claims.IssuedAt || claims.IssuedAt > nowUnix || nowUnix >= claims.Expires {
		return 0, ErrInvalidToken
	}
	return userID, nil
}

func decodeClaims(encoded []byte) (tokenClaims, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return tokenClaims{}, err
	}
	for _, required := range []string{"sub", "iat", "exp"} {
		if _, exists := fields[required]; !exists {
			return tokenClaims{}, fmt.Errorf("missing claim %s", required)
		}
	}

	var claims tokenClaims
	if err := json.Unmarshal(encoded, &claims); err != nil {
		return tokenClaims{}, err
	}
	if claims.Subject == "" {
		return tokenClaims{}, errors.New("empty subject")
	}
	return claims, nil
}

func (manager *TokenManager) sign(value string) []byte {
	mac := hmac.New(sha256.New, manager.secret)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func encodeSegment(value []byte) string {
	return base64.RawURLEncoding.EncodeToString(value)
}

func decodeSegment(value string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(value)
}
