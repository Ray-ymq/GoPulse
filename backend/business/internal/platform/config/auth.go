package config

import (
	"strings"
	"time"
)

const (
	defaultAuthJWTTTL     = 2 * time.Hour
	defaultAuthCookieName = "gopulse_session"
	defaultResourceAuth   = 16
	minimumJWTSecretBytes = 32
	minimumAuthJWTTTL     = 5 * time.Minute
	maximumAuthJWTTTL     = 24 * time.Hour
)

type AuthConfig struct {
	JWTSecret    string
	JWTTTL       time.Duration
	CookieName   string
	CookieSecure bool
}

func isCookieName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		character := name[i]
		if character < 0x21 || character > 0x7e || strings.ContainsRune("()<>@,;:\\\"/[]?={} \t", rune(character)) {
			return false
		}
	}
	return true
}
