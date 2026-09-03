package middleware

import (
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/backend/internal/user"
	"github.com/gin-gonic/gin"
)

type fakeCurrentUserFinder struct {
	record user.User
	err    error
	calls  int
}

func (finder *fakeCurrentUserFinder) FindByID(context.Context, uint64) (user.User, error) {
	finder.calls++
	return finder.record, finder.err
}

func TestRequireAdminEnforcesAuthenticationAndCurrentDatabaseRole(t *testing.T) {
	tests := []struct {
		name           string
		userID         uint64
		finder         *fakeCurrentUserFinder
		wantStatus     int
		wantCode       string
		wantDownstream int
	}{
		{name: "unauthenticated", finder: &fakeCurrentUserFinder{}, wantStatus: stdhttp.StatusUnauthorized, wantCode: "authentication_required"},
		{name: "ordinary user", userID: 17, finder: &fakeCurrentUserFinder{record: user.User{ID: 17, Role: user.RoleUser}}, wantStatus: stdhttp.StatusForbidden, wantCode: "permission_denied"},
		{name: "administrator", userID: 17, finder: &fakeCurrentUserFinder{record: user.User{ID: 17, Role: user.RoleAdmin}}, wantStatus: stdhttp.StatusNoContent, wantDownstream: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			downstreamCalls := 0
			router := gin.New()
			if test.userID > 0 {
				router.Use(func(c *gin.Context) {
					ctx := context.WithValue(c.Request.Context(), currentUserIDKey{}, test.userID)
					c.Request = c.Request.WithContext(ctx)
					c.Next()
				})
			}
			router.Use(RequireAdmin(test.finder))
			router.GET("/admin", func(c *gin.Context) {
				downstreamCalls++
				c.Status(stdhttp.StatusNoContent)
			})

			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(stdhttp.MethodGet, "/admin", nil))

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantCode != "" && !strings.Contains(response.Body.String(), `"code":"`+test.wantCode+`"`) {
				t.Fatalf("body = %s, want code %q", response.Body.String(), test.wantCode)
			}
			if downstreamCalls != test.wantDownstream {
				t.Fatalf("downstream calls = %d, want %d", downstreamCalls, test.wantDownstream)
			}
			wantFinderCalls := 0
			if test.userID > 0 {
				wantFinderCalls = 1
			}
			if test.finder.calls != wantFinderCalls {
				t.Fatalf("finder calls = %d, want %d", test.finder.calls, wantFinderCalls)
			}
		})
	}
}

func TestRequireAdminRejectsLookupFailures(t *testing.T) {
	for _, test := range []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "deleted user", err: user.ErrNotFound, wantStatus: stdhttp.StatusUnauthorized, wantCode: "authentication_required"},
		{name: "database failure", err: errors.New("database unavailable"), wantStatus: stdhttp.StatusInternalServerError, wantCode: "internal_error"},
	} {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			finder := &fakeCurrentUserFinder{err: test.err}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), currentUserIDKey{}, uint64(17)))
				c.Next()
			}, RequireAdmin(finder))
			router.GET("/admin", func(c *gin.Context) { t.Fatal("downstream handler must not run") })

			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(stdhttp.MethodGet, "/admin", nil))
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), `"code":"`+test.wantCode+`"`) {
				t.Fatalf("status=%d body=%s, want status=%d code=%s", response.Code, response.Body.String(), test.wantStatus, test.wantCode)
			}
		})
	}
}
