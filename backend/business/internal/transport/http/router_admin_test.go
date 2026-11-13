package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/backend/internal/modules/user"
	adminhttp "github.com/Ray-ymq/GoPulse/backend/internal/transport/http/handlers/admin"
	"github.com/Ray-ymq/GoPulse/backend/internal/transport/http/middleware"
	"github.com/gin-gonic/gin"
)

type managedUsersStub struct {
	adminhttp.Users
	role      user.Role
	setupErr  error
	changeErr error
	called    bool
	target    uint64
	next      user.Role
	actor     uint64
	requestID string
}

func (s *managedUsersStub) FindByID(context.Context, uint64) (user.User, error) {
	return user.User{ID: 7, Role: s.role}, nil
}
func (s *managedUsersStub) BootstrapID(context.Context) (uint64, error) { return 1, s.setupErr }
func (s *managedUsersStub) FindByUsername(_ context.Context, name string) (user.User, error) {
	s.called = true
	if name != "alice" {
		return user.User{}, user.ErrNotFound
	}
	return user.User{ID: 42, Username: name, PasswordHash: "private-hash"}, nil
}
func (s *managedUsersStub) ManagedByID(context.Context, uint64) (user.Managed, error) {
	return user.Managed{Public: user.Public{ID: 42, Username: "alice", Role: user.RoleUser}}, nil
}
func (s *managedUsersStub) ChangeRole(_ context.Context, actor, target uint64, next user.Role, requestID string) (user.RoleChange, error) {
	s.called, s.actor, s.target, s.next, s.requestID = true, actor, target, next, requestID
	return user.RoleChange{User: user.Managed{Public: user.Public{ID: target, Role: next}}, Changed: true}, s.changeErr
}
func adminRouter(s *managedUsersStub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	return NewRouter(Dependencies{}, APIRoutes{AdminUsers: adminhttp.NewHandler(s), Authentication: middleware.RequireAuthentication("session", userTokenVerifier{})})
}
func adminRequest(router *gin.Engine, method, path, body string, authenticated bool, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if authenticated {
		req.AddCookie(&http.Cookie{Name: "session", Value: "valid"})
	}
	result := httptest.NewRecorder()
	router.ServeHTTP(result, req)
	return result
}
func TestAdminRoutesRejectUnauthorizedRequests(t *testing.T) {
	for _, endpoint := range []struct{ method, path, body string }{
		{"GET", "/api/v1/admin/users/by-username/alice", ""},
		{"PATCH", "/api/v1/admin/users/42/role", `{"role":"super_admin"}`},
	} {
		for _, tc := range []struct {
			name          string
			authenticated bool
			role          user.Role
			setup         error
			status        int
		}{
			{"未登录", false, user.RoleSuperAdmin, nil, 401},
			{"普通用户", true, user.RoleUser, nil, 403},
			{"未初始化", true, user.RoleSuperAdmin, user.ErrSetupUnavailable, 503},
			{"存储故障", true, user.RoleSuperAdmin, errors.New("private database error"), 500},
		} {
			t.Run(endpoint.method+tc.name, func(t *testing.T) {
				s := &managedUsersStub{role: tc.role, setupErr: tc.setup}
				result := adminRequest(adminRouter(s), endpoint.method, endpoint.path, endpoint.body, tc.authenticated, "")
				if result.Code != tc.status || s.called || strings.Contains(result.Body.String(), "private") {
					t.Fatalf("权限校验失败：%d %s called=%v", result.Code, result.Body, s.called)
				}
			})
		}
	}
}
func TestAdminRoleChangeValidatesInputAndPreservesErrors(t *testing.T) {
	for _, tc := range []struct {
		name, id, body, origin string
		err                    error
		status                 int
		called                 bool
	}{
		{name: "提升", id: "42", body: `{"role":"super_admin"}`, status: 200, called: true},
		{name: "撤销", id: "42", body: `{"role":"user"}`, status: 200, called: true},
		{name: "非法角色", id: "42", body: `{"role":"admin"}`, status: 400},
		{name: "缺少角色", id: "42", body: `{}`, status: 400},
		{name: "空值", id: "42", body: `null`, status: 400},
		{name: "伪造操作者", id: "42", body: `{"role":"super_admin","actor":1}`, status: 400},
		{name: "非法ID", id: "0", body: `{"role":"user"}`, status: 400},
		{name: "跨站请求", id: "42", body: `{"role":"super_admin"}`, origin: "https://other.example", status: 403},
		{name: "初始管理员保护", id: "1", body: `{"role":"user"}`, err: user.ErrBootstrapProtected, status: 409, called: true},
		{name: "目标不存在", id: "42", body: `{"role":"user"}`, err: user.ErrNotFound, status: 404, called: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &managedUsersStub{role: user.RoleSuperAdmin, changeErr: tc.err}
			result := adminRequest(adminRouter(s), "PATCH", "/api/v1/admin/users/"+tc.id+"/role", tc.body, true, tc.origin)
			if result.Code != tc.status || s.called != tc.called {
				t.Fatalf("响应不符合预期：%d %s called=%v", result.Code, result.Body, s.called)
			}
			if tc.status == 200 && (s.actor != 7 || s.target != 42 || s.requestID == "" || s.requestID != result.Header().Get("X-Request-ID") || !strings.Contains(result.Body.String(), `"changed":true`) || !strings.Contains(tc.body, string(s.next))) {
				t.Fatalf("角色变更参数或响应不正确：%+v %s", s, result.Body)
			}
		})
	}
}
func TestAdminLookupAndRevokedRole(t *testing.T) {
	s := &managedUsersStub{role: user.RoleSuperAdmin}
	router := adminRouter(s)
	result := adminRequest(router, "GET", "/api/v1/admin/users/by-username/alice", "", true, "")
	if result.Code != 200 || !strings.Contains(result.Body.String(), `"is_bootstrap_super_admin":false`) || strings.Contains(result.Body.String(), "hash") {
		t.Fatalf("账号响应不正确：%d %s", result.Code, result.Body)
	}
	result = adminRequest(router, "GET", "/api/v1/admin/users/by-username/missing", "", true, "")
	if result.Code != 404 {
		t.Fatalf("不存在的账号返回 %d", result.Code)
	}
	s.called = false
	result = adminRequest(router, "GET", "/api/v1/admin/users/by-username/a!", "", true, "")
	if result.Code != 400 || s.called {
		t.Fatal("非法用户名未被拦截")
	}
	s.role = user.RoleUser
	result = adminRequest(router, "PATCH", "/api/v1/admin/users/42/role", `{"role":"super_admin"}`, true, "")
	if result.Code != 403 || s.called {
		t.Fatal("旧会话在权限撤销后仍能修改角色")
	}
}
