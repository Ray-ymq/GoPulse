package auth

import (
	"math"
	stdhttp "net/http"
	"time"
)

type CookieManager struct {
	name   string
	secure bool
	ttl    time.Duration
	now    clockFunc
}

func NewCookieManager(name string, secure bool, ttl time.Duration, now func() time.Time) *CookieManager {
	if now == nil {
		now = time.Now
	}
	return &CookieManager{name: name, secure: secure, ttl: ttl, now: now}
}

func (manager *CookieManager) Name() string {
	return manager.name
}

func (manager *CookieManager) Set(response stdhttp.ResponseWriter, token string) {
	stdhttp.SetCookie(response, &stdhttp.Cookie{
		Name:     manager.name,
		Value:    token,
		Path:     "/",
		Expires:  manager.now().UTC().Add(manager.ttl),
		MaxAge:   int(math.Ceil(manager.ttl.Seconds())),
		HttpOnly: true,
		Secure:   manager.secure,
		SameSite: stdhttp.SameSiteLaxMode,
	})
}

func (manager *CookieManager) Clear(response stdhttp.ResponseWriter) {
	stdhttp.SetCookie(response, &stdhttp.Cookie{
		Name:     manager.name,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   manager.secure,
		SameSite: stdhttp.SameSiteLaxMode,
	})
}
