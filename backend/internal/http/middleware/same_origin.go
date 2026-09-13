package middleware

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/gin-gonic/gin"
)

// SameOrigin rejects browser cross-origin mutations, including login/logout.
// Non-browser clients without Origin/Fetch Metadata keep the existing API contract.
// The maintained product edge preserves Host and overwrites X-Forwarded-Proto;
// Backend is not publicly exposed and does not trust arbitrary proxy host headers.
func SameOrigin() gin.HandlerFunc {
	return func(c *gin.Context) {
		r := c.Request
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			c.Next()
			return
		}
		allowed := true
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded == "http" || forwarded == "https" {
				scheme = forwarded
			}
			allowed = err == nil && u.Scheme == scheme && strings.EqualFold(u.Host, r.Host) && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == ""
		} else if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			allowed = false
		}
		if !allowed {
			response.Error(c, apperror.New(apperror.CodePermissionDenied, "cross-origin request denied"))
			c.Abort()
			return
		}
		c.Next()
	}
}
