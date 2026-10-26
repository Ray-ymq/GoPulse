package http

import (
	"context"
	stdhttp "net/http"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/comment"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/like"
	"github.com/gin-gonic/gin"
)

type fakeCommentApplication struct {
	create func(context.Context, uint64, uint64, comment.CreateInput) (comment.Comment, error)
	list   func(context.Context, uint64, comment.ListOptions) (comment.Page, error)
}

func (application *fakeCommentApplication) Create(ctx context.Context, postID, userID uint64, input comment.CreateInput) (comment.Comment, error) {
	if application.create == nil {
		panic("unexpected comment Create call")
	}
	return application.create(ctx, postID, userID, input)
}

func (application *fakeCommentApplication) List(ctx context.Context, postID uint64, options comment.ListOptions) (comment.Page, error) {
	if application.list == nil {
		panic("unexpected comment List call")
	}
	return application.list(ctx, postID, options)
}

type fakeLikeApplication struct {
	put    func(context.Context, uint64, uint64) error
	delete func(context.Context, uint64, uint64) error
}

func (application *fakeLikeApplication) Like(ctx context.Context, postID, userID uint64) error {
	if application.put == nil {
		panic("unexpected Like call")
	}
	return application.put(ctx, postID, userID)
}

func (application *fakeLikeApplication) Unlike(ctx context.Context, postID, userID uint64) error {
	if application.delete == nil {
		panic("unexpected Unlike call")
	}
	return application.delete(ctx, postID, userID)
}

func TestCommentAndLikeRoutesExposeContracts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	createdAt := time.Date(2026, 9, 1, 13, 0, 0, 123456000, time.UTC)
	record := comment.Comment{
		ID:        41,
		PostID:    31,
		Content:   "Nice post",
		CreatedAt: createdAt,
		Author:    comment.Author{ID: 17, Username: "alice"},
	}
	nextCursor := "opaque-comment-cursor"
	comments := &fakeCommentApplication{
		create: func(_ context.Context, postID, userID uint64, input comment.CreateInput) (comment.Comment, error) {
			if postID != 31 || userID != 17 || input.Content != "Nice post" {
				t.Fatalf("Create() post=%d user=%d input=%#v", postID, userID, input)
			}
			return record, nil
		},
		list: func(_ context.Context, postID uint64, options comment.ListOptions) (comment.Page, error) {
			if postID != 31 || options.Limit != comment.DefaultLimit || options.Cursor != nil {
				t.Fatalf("List() post=%d options=%#v", postID, options)
			}
			return comment.Page{Comments: []comment.Comment{record}, NextCursor: &nextCursor}, nil
		},
	}
	likes := &fakeLikeApplication{
		put: func(_ context.Context, postID, userID uint64) error {
			if postID != 31 || userID != 17 {
				t.Fatalf("Like() post=%d user=%d", postID, userID)
			}
			return nil
		},
		delete: func(_ context.Context, postID, userID uint64) error {
			if postID != 31 || userID != 17 {
				t.Fatalf("Unlike() post=%d user=%d", postID, userID)
			}
			return nil
		},
	}
	router := commentLikeRouter(comments, likes)
	cookie := &stdhttp.Cookie{Name: "session", Value: "valid-token"}

	created := performJSONRequest(router, stdhttp.MethodPost, "/api/v1/posts/31/comments", `{"content":"Nice post"}`, cookie)
	if created.Code != stdhttp.StatusCreated {
		t.Fatalf("create comment status=%d body=%s", created.Code, created.Body.String())
	}
	assertJSONEqual(t, created.Body.String(), `{"data":{"id":41,"post_id":31,"content":"Nice post","created_at":"2026-09-01T13:00:00.123456Z","author":{"id":17,"username":"alice"}}}`)

	listed := performJSONRequest(router, stdhttp.MethodGet, "/api/v1/posts/31/comments", "", cookie)
	if listed.Code != stdhttp.StatusOK {
		t.Fatalf("list comments status=%d body=%s", listed.Code, listed.Body.String())
	}
	assertJSONEqual(t, listed.Body.String(), `{"data":[{"id":41,"post_id":31,"content":"Nice post","created_at":"2026-09-01T13:00:00.123456Z","author":{"id":17,"username":"alice"}}],"meta":{"next_cursor":"opaque-comment-cursor"}}`)

	for _, method := range []string{stdhttp.MethodPut, stdhttp.MethodDelete} {
		response := performJSONRequest(router, method, "/api/v1/posts/31/like", "", cookie)
		if response.Code != stdhttp.StatusNoContent || response.Body.Len() != 0 {
			t.Fatalf("%s like status=%d body=%q", method, response.Code, response.Body.String())
		}
	}
}

func TestCommentAndLikeRoutesRequireAuthentication(t *testing.T) {
	router := commentLikeRouter(&fakeCommentApplication{}, &fakeLikeApplication{})
	for _, request := range []struct {
		method string
		path   string
		body   string
	}{
		{method: stdhttp.MethodPost, path: "/api/v1/posts/31/comments", body: `{"content":"Nice post"}`},
		{method: stdhttp.MethodGet, path: "/api/v1/posts/31/comments"},
		{method: stdhttp.MethodPut, path: "/api/v1/posts/31/like"},
		{method: stdhttp.MethodDelete, path: "/api/v1/posts/31/like"},
	} {
		response := performJSONRequest(router, request.method, request.path, request.body, nil)
		if response.Code != stdhttp.StatusUnauthorized {
			t.Fatalf("%s %s status=%d body=%s", request.method, request.path, response.Code, response.Body.String())
		}
		assertJSONEqual(t, response.Body.String(), `{"error":{"code":"authentication_required","message":"authentication is required"}}`)
	}
}

func TestCommentRoutesRejectInvalidBodiesPaginationAndIDs(t *testing.T) {
	router := commentLikeRouter(&fakeCommentApplication{}, &fakeLikeApplication{})
	cookie := &stdhttp.Cookie{Name: "session", Value: "valid-token"}
	requests := []struct {
		method string
		path   string
		body   string
	}{
		{method: stdhttp.MethodPost, path: "/api/v1/posts/31/comments", body: `{"content":"Nice","author_id":999}`},
		{method: stdhttp.MethodPost, path: "/api/v1/posts/31/comments", body: `{"content":"` + strings.Repeat("a", 70<<10) + `"}`},
		{method: stdhttp.MethodGet, path: "/api/v1/posts/31/comments?limit=0"},
		{method: stdhttp.MethodGet, path: "/api/v1/posts/31/comments?limit=51"},
		{method: stdhttp.MethodGet, path: "/api/v1/posts/31/comments?cursor=damaged"},
		{method: stdhttp.MethodPost, path: "/api/v1/posts/0/comments", body: `{"content":"Nice"}`},
		{method: stdhttp.MethodGet, path: "/api/v1/posts/not-a-number/comments"},
		{method: stdhttp.MethodPut, path: "/api/v1/posts/0/like"},
		{method: stdhttp.MethodDelete, path: "/api/v1/posts/not-a-number/like"},
	}
	for _, request := range requests {
		response := performJSONRequest(router, request.method, request.path, request.body, cookie)
		if response.Code != stdhttp.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"validation_failed"`) {
			t.Fatalf("%s %s status=%d body=%s", request.method, request.path, response.Code, response.Body.String())
		}
	}
}

func TestCommentAndLikeRoutesMapMissingPost(t *testing.T) {
	missing := apperror.New(apperror.CodePostNotFound, "post not found")
	comments := &fakeCommentApplication{
		create: func(context.Context, uint64, uint64, comment.CreateInput) (comment.Comment, error) {
			return comment.Comment{}, missing
		},
		list: func(context.Context, uint64, comment.ListOptions) (comment.Page, error) {
			return comment.Page{}, missing
		},
	}
	likes := &fakeLikeApplication{
		put:    func(context.Context, uint64, uint64) error { return missing },
		delete: func(context.Context, uint64, uint64) error { return missing },
	}
	router := commentLikeRouter(comments, likes)
	cookie := &stdhttp.Cookie{Name: "session", Value: "valid-token"}
	for _, request := range []struct {
		method string
		path   string
		body   string
	}{
		{method: stdhttp.MethodPost, path: "/api/v1/posts/999/comments", body: `{"content":"Nice"}`},
		{method: stdhttp.MethodGet, path: "/api/v1/posts/999/comments"},
		{method: stdhttp.MethodPut, path: "/api/v1/posts/999/like"},
		{method: stdhttp.MethodDelete, path: "/api/v1/posts/999/like"},
	} {
		response := performJSONRequest(router, request.method, request.path, request.body, cookie)
		if response.Code != stdhttp.StatusNotFound {
			t.Fatalf("%s %s status=%d body=%s", request.method, request.path, response.Code, response.Body.String())
		}
		assertJSONEqual(t, response.Body.String(), `{"error":{"code":"post_not_found","message":"post not found"}}`)
	}
}

func commentLikeRouter(comments comment.Application, likes like.Application) *gin.Engine {
	return NewRouter(Dependencies{}, APIRoutes{
		Comments:       comment.NewHandler(comments),
		Likes:          like.NewHandler(likes),
		Authentication: middleware.RequireAuthentication("session", acceptingVerifier{userID: 17}),
	})
}
