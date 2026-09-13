package middleware

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSameOriginMutation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, origin, site string
		want               int
	}{
		{"edge with port", "http://localhost:18080", "same-origin", 204},
		{"cross origin", "https://evil.example", "cross-site", 403},
		{"same site sibling", "http://other.localhost:18080", "same-site", 403},
		{"opaque origin", "null", "cross-site", 403},
		{"metadata only", "", "cross-site", 403},
		{"non browser client", "", "", 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.Use(SameOrigin())
			router.POST("/auth/logout", func(c *gin.Context) { c.Status(204) })
			r := httptest.NewRequest(http.MethodPost, "http://localhost:18080/auth/logout", nil)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status %d, want %d", w.Code, tc.want)
			}
		})
	}
}
