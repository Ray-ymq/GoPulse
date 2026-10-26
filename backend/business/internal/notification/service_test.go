package notification

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
)

type fakeStore struct {
	list     func(context.Context, uint64, ListOptions) ([]Public, error)
	markRead func(context.Context, uint64, uint64) error
}

func (store *fakeStore) ListByRecipient(ctx context.Context, recipientID uint64, options ListOptions) ([]Public, error) {
	return store.list(ctx, recipientID, options)
}
func (store *fakeStore) MarkRead(ctx context.Context, recipientID, notificationID uint64) error {
	return store.markRead(ctx, recipientID, notificationID)
}

func TestServiceListsRecipientPageAndBuildsCursor(t *testing.T) {
	createdAt := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	store := &fakeStore{
		list: func(_ context.Context, recipientID uint64, options ListOptions) ([]Public, error) {
			if recipientID != 7 || options.Limit != 2 {
				t.Fatalf("ListByRecipient(%d, %#v)", recipientID, options)
			}
			return []Public{
				{ID: 3, Type: bus.PostLiked, CreatedAt: createdAt.Add(2 * time.Second)},
				{ID: 2, Type: bus.CommentCreated, CreatedAt: createdAt.Add(time.Second)},
				{ID: 1, Type: bus.PostLiked, CreatedAt: createdAt},
			}, nil
		},
		markRead: func(context.Context, uint64, uint64) error { return nil },
	}
	page, err := NewService(store).List(context.Background(), 7, ListOptions{Limit: 2})
	if err != nil || len(page.Notifications) != 2 || page.NextCursor == nil {
		t.Fatalf("List() page=%#v error=%v", page, err)
	}
	cursor, err := DecodeCursor(*page.NextCursor)
	if err != nil || cursor.ID != 2 || !cursor.CreatedAt.Equal(createdAt.Add(time.Second)) {
		t.Fatalf("next cursor=%#v error=%v", cursor, err)
	}
}

func TestServiceMapsRecipientScopedMissingReadToSafeNotFound(t *testing.T) {
	store := &fakeStore{
		list: func(context.Context, uint64, ListOptions) ([]Public, error) { return nil, nil },
		markRead: func(_ context.Context, recipientID, notificationID uint64) error {
			if recipientID != 7 || notificationID != 99 {
				t.Fatalf("MarkRead(%d, %d)", recipientID, notificationID)
			}
			return ErrNotFound
		},
	}
	err := NewService(store).MarkRead(context.Background(), 7, 99)
	appError, ok := apperror.As(err)
	if !ok || appError.Code != apperror.CodeNotificationNotFound || !errors.Is(err, appError) {
		t.Fatalf("MarkRead() error=%v", err)
	}
}
