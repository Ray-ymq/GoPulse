package http

import (
	"context"
	stdhttp "net/http"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/gin-gonic/gin"
)

type fakePostApplication struct {
	create func(context.Context, uint64, post.CreateInput) (post.Post, error)
	list   func(context.Context, uint64, post.ListOptions) (post.Page, error)
	detail func(context.Context, uint64, uint64) (post.Post, error)
}

func (application *fakePostApplication) Create(ctx context.Context, userID uint64, input post.CreateInput) (post.Post, error) {
	return application.create(ctx, userID, input)
}

func (application *fakePostApplication) List(ctx context.Context, userID uint64, options post.ListOptions) (post.Page, error) {
	return application.list(ctx, userID, options)
}

func (application *fakePostApplication) Detail(ctx context.Context, postID, userID uint64) (post.Post, error) {
	return application.detail(ctx, postID, userID)
}

func TestPostRoutesExposeCreateListAndDetailContracts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	createdAt := time.Date(2026, 9, 1, 12, 0, 0, 123456000, time.UTC)
	record := post.Post{
		ID:           31,
		Title:        "First post",
		Content:      "Hello GoPulse",
		CreatedAt:    createdAt,
		UpdatedAt:    createdAt,
		Author:       post.Author{ID: 17, Username: "alice"},
		CommentCount: 2,
		LikeCount:    3,
		LikedByMe:    true,
	}
	nextCursor := "opaque-next-cursor"
	application := &fakePostApplication{
		create: func(_ context.Context, userID uint64, input post.CreateInput) (post.Post, error) {
			if userID != 17 || input.Title != record.Title || input.Content != record.Content {
				t.Fatalf("Create() user=%d input=%#v", userID, input)
			}
			return record, nil
		},
		list: func(_ context.Context, userID uint64, options post.ListOptions) (post.Page, error) {
			if userID != 17 || options.Limit != post.DefaultLimit || options.Cursor != nil {
				t.Fatalf("List() user=%d options=%#v", userID, options)
			}
			return post.Page{Posts: []post.Post{record}, NextCursor: &nextCursor}, nil
		},
		detail: func(_ context.Context, postID, userID uint64) (post.Post, error) {
			if postID != 31 || userID != 17 {
				t.Fatalf("Detail() post=%d user=%d", postID, userID)
			}
			return record, nil
		},
	}
	router := postRouter(application)
	cookie := &stdhttp.Cookie{Name: "session", Value: "valid-token"}

	created := performJSONRequest(router, stdhttp.MethodPost, "/api/v1/posts", `{"title":"First post","content":"Hello GoPulse"}`, cookie)
	if created.Code != stdhttp.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	assertJSONEqual(t, created.Body.String(), `{"data":{"id":31,"title":"First post","content":"Hello GoPulse","created_at":"2026-09-01T12:00:00.123456Z","updated_at":"2026-09-01T12:00:00.123456Z","author":{"id":17,"username":"alice"},"comment_count":2,"like_count":3,"liked_by_me":true}}`)

	listed := performJSONRequest(router, stdhttp.MethodGet, "/api/v1/posts", "", cookie)
	if listed.Code != stdhttp.StatusOK {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	assertJSONEqual(t, listed.Body.String(), `{"data":[{"id":31,"title":"First post","content":"Hello GoPulse","created_at":"2026-09-01T12:00:00.123456Z","updated_at":"2026-09-01T12:00:00.123456Z","author":{"id":17,"username":"alice"},"comment_count":2,"like_count":3,"liked_by_me":true}],"meta":{"next_cursor":"opaque-next-cursor"}}`)

	detail := performJSONRequest(router, stdhttp.MethodGet, "/api/v1/posts/31", "", cookie)
	if detail.Code != stdhttp.StatusOK {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	assertJSONEqual(t, detail.Body.String(), `{"data":{"id":31,"title":"First post","content":"Hello GoPulse","created_at":"2026-09-01T12:00:00.123456Z","updated_at":"2026-09-01T12:00:00.123456Z","author":{"id":17,"username":"alice"},"comment_count":2,"like_count":3,"liked_by_me":true}}`)
}

func TestPostRoutesRequireAuthentication(t *testing.T) {
	application := unexpectedPostApplication(t)
	router := postRouter(application)
	for _, request := range []struct {
		method string
		path   string
		body   string
	}{
		{method: stdhttp.MethodPost, path: "/api/v1/posts", body: `{"title":"title","content":"content"}`},
		{method: stdhttp.MethodGet, path: "/api/v1/posts"},
		{method: stdhttp.MethodGet, path: "/api/v1/posts/1"},
	} {
		response := performJSONRequest(router, request.method, request.path, request.body, nil)
		if response.Code != stdhttp.StatusUnauthorized {
			t.Fatalf("%s %s status=%d body=%s", request.method, request.path, response.Code, response.Body.String())
		}
		assertJSONEqual(t, response.Body.String(), `{"error":{"code":"authentication_required","message":"authentication is required"}}`)
	}
}

func TestPostRoutesRejectUnknownAuthorAndInvalidPaginationOrID(t *testing.T) {
	application := unexpectedPostApplication(t)
	router := postRouter(application)
	cookie := &stdhttp.Cookie{Name: "session", Value: "valid-token"}
	requests := []struct {
		method string
		path   string
		body   string
	}{
		{method: stdhttp.MethodPost, path: "/api/v1/posts", body: `{"title":"title","content":"content","author_id":999}`},
		{method: stdhttp.MethodGet, path: "/api/v1/posts?limit=0"},
		{method: stdhttp.MethodGet, path: "/api/v1/posts?limit=51"},
		{method: stdhttp.MethodGet, path: "/api/v1/posts?cursor=damaged"},
		{method: stdhttp.MethodGet, path: "/api/v1/posts/0"},
		{method: stdhttp.MethodGet, path: "/api/v1/posts/not-a-number"},
	}
	for _, request := range requests {
		response := performJSONRequest(router, request.method, request.path, request.body, cookie)
		if response.Code != stdhttp.StatusBadRequest {
			t.Fatalf("%s %s status=%d body=%s", request.method, request.path, response.Code, response.Body.String())
		}
		var expectedCode = `"code":"validation_failed"`
		if !strings.Contains(response.Body.String(), expectedCode) {
			t.Fatalf("response body=%s, want %s", response.Body.String(), expectedCode)
		}
	}
}

func TestPostDetailMapsNotFound(t *testing.T) {
	application := &fakePostApplication{
		create: func(context.Context, uint64, post.CreateInput) (post.Post, error) { return post.Post{}, nil },
		list:   func(context.Context, uint64, post.ListOptions) (post.Page, error) { return post.Page{}, nil },
		detail: func(context.Context, uint64, uint64) (post.Post, error) {
			return post.Post{}, apperror.New(apperror.CodePostNotFound, "post not found")
		},
	}
	response := performJSONRequest(postRouter(application), stdhttp.MethodGet, "/api/v1/posts/999", "", &stdhttp.Cookie{Name: "session", Value: "valid-token"})
	if response.Code != stdhttp.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	assertJSONEqual(t, response.Body.String(), `{"error":{"code":"post_not_found","message":"post not found"}}`)
}

func postRouter(application post.Application) *gin.Engine {
	return NewRouter(Dependencies{}, APIRoutes{
		Posts:          post.NewHandler(application),
		Authentication: middleware.RequireAuthentication("session", acceptingVerifier{userID: 17}),
	})
}

func unexpectedPostApplication(t *testing.T) *fakePostApplication {
	t.Helper()
	return &fakePostApplication{
		create: func(context.Context, uint64, post.CreateInput) (post.Post, error) {
			t.Fatal("Create() must not be called")
			return post.Post{}, nil
		},
		list: func(context.Context, uint64, post.ListOptions) (post.Page, error) {
			t.Fatal("List() must not be called")
			return post.Page{}, nil
		},
		detail: func(context.Context, uint64, uint64) (post.Post, error) {
			t.Fatal("Detail() must not be called")
			return post.Post{}, nil
		},
	}
}
