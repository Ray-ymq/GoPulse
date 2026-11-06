package http

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
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

type fakeCommandPostApplication struct {
	*fakePostApplication
	createWithCommand func(context.Context, uint64, post.CreateInput, post.CommandRequest) (post.CreateResult, error)
	commandContext    func(context.Context) (post.CommandContext, error)
	commandResult     func(context.Context, uint64, post.CommandRequest) (post.CreateResult, error)
}

func (application *fakeCommandPostApplication) CreateWithCommand(ctx context.Context, userID uint64, input post.CreateInput, request post.CommandRequest) (post.CreateResult, error) {
	return application.createWithCommand(ctx, userID, input, request)
}

func (application *fakeCommandPostApplication) CommandContext(ctx context.Context) (post.CommandContext, error) {
	return application.commandContext(ctx)
}

func (application *fakeCommandPostApplication) CommandResult(ctx context.Context, userID uint64, request post.CommandRequest) (post.CreateResult, error) {
	return application.commandResult(ctx, userID, request)
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
	assertJSONEqual(t, created.Body.String(), `{"data":{"id":31,"title":"First post","content":"Hello GoPulse","created_at":"2026-09-01T12:00:00.123456Z","updated_at":"2026-09-01T12:00:00.123456Z","author":{"id":17,"username":"alice","display_name":"","following":false},"comment_count":2,"like_count":3,"liked_by_me":true,"edited_at":null,"content_revision":0,"bookmarked_by_me":false}}`)

	listed := performJSONRequest(router, stdhttp.MethodGet, "/api/v1/posts", "", cookie)
	if listed.Code != stdhttp.StatusOK {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	assertJSONEqual(t, listed.Body.String(), `{"data":[{"id":31,"title":"First post","content":"Hello GoPulse","created_at":"2026-09-01T12:00:00.123456Z","updated_at":"2026-09-01T12:00:00.123456Z","author":{"id":17,"username":"alice","display_name":"","following":false},"comment_count":2,"like_count":3,"liked_by_me":true,"edited_at":null,"content_revision":0,"bookmarked_by_me":false}],"meta":{"next_cursor":"opaque-next-cursor"}}`)

	detail := performJSONRequest(router, stdhttp.MethodGet, "/api/v1/posts/31", "", cookie)
	if detail.Code != stdhttp.StatusOK {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	assertJSONEqual(t, detail.Body.String(), `{"data":{"id":31,"title":"First post","content":"Hello GoPulse","created_at":"2026-09-01T12:00:00.123456Z","updated_at":"2026-09-01T12:00:00.123456Z","author":{"id":17,"username":"alice","display_name":"","following":false},"comment_count":2,"like_count":3,"liked_by_me":true,"edited_at":null,"content_revision":0,"bookmarked_by_me":false}}`)
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
		{method: stdhttp.MethodGet, path: "/api/v1/post-commands/context"},
		{method: stdhttp.MethodGet, path: "/api/v1/post-commands/result"},
	} {
		response := performJSONRequest(router, request.method, request.path, request.body, nil)
		if response.Code != stdhttp.StatusUnauthorized {
			t.Fatalf("%s %s status=%d body=%s", request.method, request.path, response.Code, response.Body.String())
		}
		assertJSONEqual(t, response.Body.String(), `{"error":{"code":"authentication_required","message":"authentication is required"}}`)
	}
}

func TestPostCommandRoutesValidatePairedHeadersAndReturnStableMeta(t *testing.T) {
	record := post.Post{ID: 31, Title: "First post", Content: "Hello", Author: post.Author{ID: 17, Username: "alice"}}
	epoch := "123e4567-e89b-12d3-a456-426614174000"
	key := "123e4567-e89b-12d3-a456-426614174001"
	application := &fakeCommandPostApplication{
		fakePostApplication: unexpectedPostApplication(t),
		createWithCommand: func(_ context.Context, userID uint64, input post.CreateInput, request post.CommandRequest) (post.CreateResult, error) {
			if userID != 17 || input.Title != "First post" || request.DataEpoch != epoch || request.IdempotencyKey != key {
				t.Fatalf("command request user=%d input=%#v request=%#v", userID, input, request)
			}
			return post.CreateResult{Post: &record, Meta: post.CommandMeta{
				CommandID: "123e4567-e89b-12d3-a456-426614174002", EventID: "123e4567-e89b-12d3-a456-426614174003",
				DataEpoch: epoch, PostID: record.ID, CommittedAt: time.Date(2026, 9, 2, 3, 4, 5, 0, time.UTC), ResourceState: "available",
			}}, nil
		},
		commandContext: func(context.Context) (post.CommandContext, error) {
			return post.CommandContext{DataEpoch: epoch, RetryWindow: 604800, RetryWindowUntil: time.Date(2026, 9, 9, 3, 4, 5, 0, time.UTC)}, nil
		},
		commandResult: func(_ context.Context, userID uint64, request post.CommandRequest) (post.CreateResult, error) {
			if userID != 17 || request.DataEpoch != epoch || request.IdempotencyKey != key {
				t.Fatalf("result request user=%d request=%#v", userID, request)
			}
			return post.CreateResult{Meta: post.CommandMeta{
				CommandID: "123e4567-e89b-12d3-a456-426614174002", EventID: "123e4567-e89b-12d3-a456-426614174003",
				DataEpoch: epoch, PostID: record.ID, CommittedAt: time.Date(2026, 9, 2, 3, 4, 5, 0, time.UTC), Replayed: true, ResourceState: "unavailable",
			}}, nil
		},
	}
	router := postRouter(application)
	cookie := &stdhttp.Cookie{Name: "session", Value: "valid-token"}

	contextResponse := performJSONRequest(router, stdhttp.MethodGet, "/api/v1/post-commands/context", "", cookie)
	if contextResponse.Code != stdhttp.StatusOK {
		t.Fatalf("context status=%d body=%s", contextResponse.Code, contextResponse.Body.String())
	}
	assertJSONEqual(t, contextResponse.Body.String(), `{"data":{"data_epoch":"123e4567-e89b-12d3-a456-426614174000","retry_window_seconds":604800,"retry_window_until":"2026-09-09T03:04:05Z"}}`)

	missing := performJSONRequestWithHeaders(router, stdhttp.MethodPost, "/api/v1/posts", `{"title":"First post","content":"Hello"}`, cookie, map[string]string{"Idempotency-Key": key})
	if missing.Code != stdhttp.StatusBadRequest {
		t.Fatalf("missing paired header status=%d body=%s", missing.Code, missing.Body.String())
	}
	assertJSONEqual(t, missing.Body.String(), `{"error":{"code":"validation_failed","message":"Idempotency-Key and X-GoPulse-Data-Epoch must be supplied together"}}`)

	created := performJSONRequestWithHeaders(router, stdhttp.MethodPost, "/api/v1/posts", `{"title":"First post","content":"Hello"}`, cookie, map[string]string{"Idempotency-Key": key, "X-GoPulse-Data-Epoch": epoch})
	if created.Code != stdhttp.StatusCreated {
		t.Fatalf("command create status=%d body=%s", created.Code, created.Body.String())
	}
	assertJSONEqual(t, created.Body.String(), `{"data":{"id":31,"title":"First post","content":"Hello","created_at":"0001-01-01T00:00:00Z","updated_at":"0001-01-01T00:00:00Z","author":{"id":17,"username":"alice","display_name":"","following":false},"comment_count":0,"like_count":0,"liked_by_me":false,"edited_at":null,"content_revision":0,"bookmarked_by_me":false},"meta":{"command_id":"123e4567-e89b-12d3-a456-426614174002","event_id":"123e4567-e89b-12d3-a456-426614174003","data_epoch":"123e4567-e89b-12d3-a456-426614174000","post_id":31,"committed_at":"2026-09-02T03:04:05Z","replayed":false,"resource_state":"available"}}`)

	result := performJSONRequestWithHeaders(router, stdhttp.MethodGet, "/api/v1/post-commands/result", "", cookie, map[string]string{"Idempotency-Key": key, "X-GoPulse-Data-Epoch": epoch})
	if result.Code != stdhttp.StatusOK {
		t.Fatalf("command result status=%d body=%s", result.Code, result.Body.String())
	}
	assertJSONEqual(t, result.Body.String(), `{"data":null,"meta":{"command_id":"123e4567-e89b-12d3-a456-426614174002","event_id":"123e4567-e89b-12d3-a456-426614174003","data_epoch":"123e4567-e89b-12d3-a456-426614174000","post_id":31,"committed_at":"2026-09-02T03:04:05Z","replayed":true,"resource_state":"unavailable"}}`)
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

func performJSONRequestWithHeaders(handler stdhttp.Handler, method, path, body string, cookie *stdhttp.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
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
