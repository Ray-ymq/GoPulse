package http

import (
	"context"
	"encoding/json"
	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/bookmark"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/gin-gonic/gin"
	"net/http"
	"testing"
	"time"
)

type bookmarkApplication struct{ calls int }

func (a *bookmarkApplication) Bookmark(_ context.Context, id, viewer uint64) error {
	a.calls++
	if viewer != 17 {
		panic("wrong viewer")
	}
	if id == 404 {
		return apperror.New(apperror.CodePostNotFound, "post not found")
	}
	return nil
}
func (a *bookmarkApplication) Unbookmark(ctx context.Context, id, viewer uint64) error {
	return a.Bookmark(ctx, id, viewer)
}
func TestBookmarkProtectedRoutesAndPrivateCursor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	actor := &bookmarkApplication{}
	viewer := uint64(17)
	listCalls := 0
	cursor, _ := post.EncodeCursor(post.Cursor{CreatedAt: time.Now().UTC(), ID: 31})
	posts := &fakePostApplication{list: func(_ context.Context, user uint64, o post.ListOptions) (post.Page, error) {
		listCalls++
		if user != viewer || !o.Bookmarks {
			t.Fatal("list must use session bookmarks")
		}
		return post.Page{Posts: []post.Post{}, NextCursor: &cursor}, nil
	}}
	router := NewRouter(Dependencies{}, APIRoutes{Posts: post.NewHandler(posts).WithBookmarkCursorSecret("test-secret"), Bookmarks: bookmark.NewHandler(actor), Authentication: middleware.RequireAuthentication("session", acceptingVerifier{userID: viewer})})
	cookie := &http.Cookie{Name: "session", Value: "valid-token"}
	for _, path := range []string{"/api/v1/bookmarks", "/api/v1/posts/31/bookmark"} {
		method := http.MethodGet
		if path != "/api/v1/bookmarks" {
			method = http.MethodPut
		}
		if got := performJSONRequest(router, method, path, "", nil); got.Code != 401 {
			t.Fatalf("anonymous status %d", got.Code)
		}
	}
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		for range 2 {
			if got := performJSONRequest(router, method, "/api/v1/posts/31/bookmark", "", cookie); got.Code != 204 {
				t.Fatal(got.Body.String())
			}
		}
		if got := performJSONRequest(router, method, "/api/v1/posts/404/bookmark", "", cookie); got.Code != 404 {
			t.Fatal(got.Body.String())
		}
	}
	for _, query := range []string{"user_id=1", "username=alice", "cursor=bad", "limit=0"} {
		if got := performJSONRequest(router, http.MethodGet, "/api/v1/bookmarks?"+query, "", cookie); got.Code != 400 {
			t.Fatal(got.Body.String())
		}
	}
	if listCalls != 0 {
		t.Fatal("invalid request reached list")
	}
	first := performJSONRequest(router, http.MethodGet, "/api/v1/bookmarks?limit=1", "", cookie)
	var page struct {
		Meta struct {
			Cursor string `json:"next_cursor"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Meta.Cursor == "" || page.Meta.Cursor == cursor {
		t.Fatal("unsigned cursor")
	}
	if got := performJSONRequest(router, http.MethodGet, "/api/v1/bookmarks?cursor="+page.Meta.Cursor, "", cookie); got.Code != 200 {
		t.Fatal(got.Body.String())
	}
	if got := performJSONRequest(router, http.MethodGet, "/api/v1/bookmarks?cursor="+page.Meta.Cursor+"x", "", cookie); got.Code != 400 {
		t.Fatal("tampered cursor accepted")
	}
	other := NewRouter(Dependencies{}, APIRoutes{Posts: post.NewHandler(posts).WithBookmarkCursorSecret("test-secret"), Authentication: middleware.RequireAuthentication("session", acceptingVerifier{userID: 18})})
	if got := performJSONRequest(other, http.MethodGet, "/api/v1/bookmarks?cursor="+page.Meta.Cursor, "", cookie); got.Code != 400 {
		t.Fatal("other viewer cursor accepted")
	}
}
