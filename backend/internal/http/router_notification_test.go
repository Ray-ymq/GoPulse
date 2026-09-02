package http

import (
	"context"
	stdhttp "net/http"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/notification"
	"github.com/gin-gonic/gin"
)

type fakeNotificationApplication struct {
	list     func(context.Context, uint64, notification.ListOptions) (notification.Page, error)
	markRead func(context.Context, uint64, uint64) error
}

func (application *fakeNotificationApplication) List(ctx context.Context, recipientID uint64, options notification.ListOptions) (notification.Page, error) {
	return application.list(ctx, recipientID, options)
}
func (application *fakeNotificationApplication) MarkRead(ctx context.Context, recipientID, notificationID uint64) error {
	return application.markRead(ctx, recipientID, notificationID)
}

func TestNotificationRoutesExposePublicShapeAndIdempotentRead(t *testing.T) {
	createdAt := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	commentID := uint64(41)
	application := &fakeNotificationApplication{
		list: func(_ context.Context, recipientID uint64, options notification.ListOptions) (notification.Page, error) {
			if recipientID != 17 || options.Limit != 20 {
				t.Fatalf("List(%d, %#v)", recipientID, options)
			}
			return notification.Page{Notifications: []notification.Public{{
				ID: 9, Type: bus.CommentCreated, CreatedAt: createdAt,
				Actor: notification.Actor{ID: 22, Username: "bob"}, PostID: 31, CommentID: &commentID,
			}}}, nil
		},
		markRead: func(_ context.Context, recipientID, notificationID uint64) error {
			if recipientID != 17 || notificationID != 9 {
				t.Fatalf("MarkRead(%d, %d)", recipientID, notificationID)
			}
			return nil
		},
	}
	router := notificationRouter(application)
	cookie := &stdhttp.Cookie{Name: "session", Value: "valid-token"}
	listed := performJSONRequest(router, stdhttp.MethodGet, "/api/v1/notifications", "", cookie)
	if listed.Code != stdhttp.StatusOK {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	assertJSONEqual(t, listed.Body.String(), `{"data":[{"id":9,"type":"comment.created","created_at":"2026-09-02T08:00:00Z","read_at":null,"actor":{"id":22,"username":"bob"},"post_id":31,"comment_id":41}],"meta":{"next_cursor":null}}`)
	if strings.Contains(listed.Body.String(), "source_event") || strings.Contains(listed.Body.String(), "recipient_id") {
		t.Fatalf("response leaked internal fields: %s", listed.Body.String())
	}
	read := performJSONRequest(router, stdhttp.MethodPatch, "/api/v1/notifications/9/read", "", cookie)
	if read.Code != stdhttp.StatusNoContent || read.Body.Len() != 0 {
		t.Fatalf("read status=%d body=%s", read.Code, read.Body.String())
	}
}

func TestNotificationRoutesProtectAndValidateRecipientOperations(t *testing.T) {
	application := &fakeNotificationApplication{
		list: func(context.Context, uint64, notification.ListOptions) (notification.Page, error) {
			t.Fatal("List() must not be called")
			return notification.Page{}, nil
		},
		markRead: func(context.Context, uint64, uint64) error {
			return apperror.New(apperror.CodeNotificationNotFound, "notification not found")
		},
	}
	if response := performJSONRequest(notificationRouter(application), stdhttp.MethodGet, "/api/v1/notifications", "", nil); response.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d body=%s", response.Code, response.Body.String())
	}
	cookie := &stdhttp.Cookie{Name: "session", Value: "valid-token"}
	for _, path := range []string{"/api/v1/notifications?limit=0", "/api/v1/notifications?cursor=damaged", "/api/v1/notifications/0/read"} {
		response := performJSONRequest(notificationRouter(application), methodForNotificationPath(path), path, "", cookie)
		if response.Code != stdhttp.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
	missing := performJSONRequest(notificationRouter(application), stdhttp.MethodPatch, "/api/v1/notifications/99/read", "", cookie)
	if missing.Code != stdhttp.StatusNotFound {
		t.Fatalf("missing status=%d body=%s", missing.Code, missing.Body.String())
	}
	assertJSONEqual(t, missing.Body.String(), `{"error":{"code":"notification_not_found","message":"notification not found"}}`)
}

func methodForNotificationPath(path string) string {
	if strings.Contains(path, "/read") {
		return stdhttp.MethodPatch
	}
	return stdhttp.MethodGet
}

func notificationRouter(application notification.Application) *gin.Engine {
	return NewRouter(Dependencies{}, APIRoutes{
		Notifications:  notification.NewHandler(application),
		Authentication: middleware.RequireAuthentication("session", acceptingVerifier{userID: 17}),
	})
}
