package http

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/modules/post"
	"github.com/Ray-ymq/GoPulse/backend/internal/modules/user"
	userhttp "github.com/Ray-ymq/GoPulse/backend/internal/transport/http/handlers/user"
	"github.com/Ray-ymq/GoPulse/backend/internal/transport/http/middleware"

	"github.com/gin-gonic/gin"
)

type userProfileStub struct{ userhttp.Profiles }

func (userProfileStub) Get(_ context.Context, username string, viewer uint64) (user.Profile, error) {
	return user.Profile{ID: 42, Username: username, IsSelf: viewer == 42}, nil
}

type userFollowStub struct {
	set       func(context.Context, uint64, uint64, bool) error
	relations func(context.Context, uint64, bool, user.RelationOptions) ([]user.Profile, *string, error)
}

func (s userFollowStub) SetFollow(ctx context.Context, viewer, target uint64, following bool) error {
	return s.set(ctx, viewer, target, following)
}
func (s userFollowStub) Relations(ctx context.Context, viewer uint64, followers bool, options user.RelationOptions) ([]user.Profile, *string, error) {
	return s.relations(ctx, viewer, followers, options)
}

type userTokenVerifier struct{}

func (userTokenVerifier) Verify(string) (uint64, error) { return 7, nil }

func userTestRouter(handler *userhttp.Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerAPIV1Routes(router, APIRoutes{Users: handler, Authentication: middleware.RequireAuthentication("session", userTokenVerifier{})})
	return router
}
func userRequest(router *gin.Engine, method, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	request.AddCookie(&stdhttp.Cookie{Name: "session", Value: "valid"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestUserPostRoutesPreserveFilters(t *testing.T) {
	for _, tc := range []struct {
		path      string
		author    uint64
		following bool
	}{
		{"/api/v1/users/alice/posts?limit=3", 42, false},
		{"/api/v1/posts/following?limit=3", 0, true},
	} {
		t.Run(tc.path, func(t *testing.T) {
			called := false
			posts := &fakePostApplication{list: func(_ context.Context, viewer uint64, options post.ListOptions) (post.Page, error) {
				called = true
				if viewer != 7 || options.AuthorID != tc.author || options.Following != tc.following || options.Limit != 3 {
					t.Fatalf("查询条件发生变化：viewer=%d options=%+v", viewer, options)
				}
				next := "next-page"
				return post.Page{Posts: []post.Post{}, NextCursor: &next}, nil
			}}
			router := userTestRouter(userhttp.NewHandler(userProfileStub{}, posts, userFollowStub{}))
			response := userRequest(router, "GET", tc.path)
			if response.Code != 200 || !called || !strings.Contains(response.Body.String(), "next-page") {
				t.Fatalf("查询响应不符合契约：%d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestUserFollowRoutesPreserveDirectionAndCursor(t *testing.T) {
	boundary := post.Cursor{CreatedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), ID: 19}
	cursor, err := post.EncodeCursor(boundary)
	if err != nil {
		t.Fatal(err)
	}
	for _, relation := range []string{"followers", "following"} {
		t.Run(relation, func(t *testing.T) {
			called := false
			follows := userFollowStub{relations: func(_ context.Context, viewer uint64, followers bool, options user.RelationOptions) ([]user.Profile, *string, error) {
				called = true
				if viewer != 7 || followers != (relation == "followers") || options.Limit != 3 || options.Cursor == nil || options.Cursor.ID != boundary.ID || !options.Cursor.CreatedAt.Equal(boundary.CreatedAt) {
					t.Fatalf("关系查询条件发生变化：viewer=%d followers=%v options=%+v", viewer, followers, options)
				}
				return []user.Profile{{ID: 42, Username: "alice"}}, &cursor, nil
			}}
			response := userRequest(userTestRouter(userhttp.NewHandler(nil, nil, follows)), "GET", "/api/v1/users/me/"+relation+"?limit=3&cursor="+cursor)
			if response.Code != 200 || !called || !strings.Contains(response.Body.String(), "alice") || !strings.Contains(response.Body.String(), cursor) {
				t.Fatalf("关系响应不符合契约：%d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestUserFollowRoutesPreserveCommands(t *testing.T) {
	for _, method := range []string{"PUT", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			called := false
			follows := userFollowStub{set: func(_ context.Context, viewer, target uint64, following bool) error {
				called = true
				if viewer != 7 || target != 42 || following != (method == "PUT") {
					t.Fatalf("关注命令发生变化：%d %d %v", viewer, target, following)
				}
				return nil
			}}
			response := userRequest(userTestRouter(userhttp.NewHandler(nil, nil, follows)), method, "/api/v1/users/42/follow")
			expected := `"following":false`
			if method == "PUT" {
				expected = `"following":true`
			}
			if response.Code != 200 || !called || !strings.Contains(response.Body.String(), expected) {
				t.Fatalf("关注响应不符合契约：%d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestUserRoutesRejectInvalidQueriesBeforeCallingDependencies(t *testing.T) {
	router := userTestRouter(userhttp.NewHandler(nil, nil, userFollowStub{}))
	for _, path := range []string{
		"/api/v1/users/me/followers?author_id=42",
		"/api/v1/users/me/following?limit=0",
		"/api/v1/posts/following?cursor=invalid",
		"/api/v1/posts/following?limit=2&limit=3",
	} {
		if response := userRequest(router, "GET", path); response.Code != 400 {
			t.Fatalf("非法参数未被拒绝：%s %d", path, response.Code)
		}
	}
	if response := userRequest(router, "PUT", "/api/v1/users/0/follow"); response.Code != 400 {
		t.Fatalf("非法用户 ID 未被拒绝：%d", response.Code)
	}
}

func TestUserRoutesWithoutFollowDependency(t *testing.T) {
	router := userTestRouter(userhttp.NewHandler(userProfileStub{}, nil))
	for _, route := range router.Routes() {
		if strings.Contains(route.Path, "/follow") {
			t.Fatalf("未配置关注依赖时注册了路由：%s", route.Path)
		}
	}
	if response := userRequest(router, "GET", "/api/v1/users/alice"); response.Code != 200 {
		t.Fatalf("资料路由不可用：%d", response.Code)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/users/alice", nil))
	if response.Code != 401 {
		t.Fatalf("未认证请求未被拒绝：%d", response.Code)
	}
}
