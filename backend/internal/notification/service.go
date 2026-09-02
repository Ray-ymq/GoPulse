package notification

import (
	"context"
	"errors"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
)

type Store interface {
	ListByRecipient(context.Context, uint64, ListOptions) ([]Public, error)
	MarkRead(context.Context, uint64, uint64) error
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (service *Service) List(ctx context.Context, recipientID uint64, options ListOptions) (Page, error) {
	if recipientID == 0 || options.Limit < 1 || options.Limit > MaximumLimit {
		return Page{}, validationError("limit must be an integer between 1 and 50")
	}
	if options.Cursor != nil && (options.Cursor.ID == 0 || options.Cursor.CreatedAt.IsZero()) {
		return Page{}, validationError("cursor is invalid")
	}
	records, err := service.store.ListByRecipient(ctx, recipientID, options)
	if err != nil {
		return Page{}, apperror.WrapInternal(err)
	}
	page := Page{Notifications: records}
	if len(records) <= options.Limit {
		return page, nil
	}
	page.Notifications = records[:options.Limit]
	last := page.Notifications[len(page.Notifications)-1]
	nextCursor, err := EncodeCursor(Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
	if err != nil {
		return Page{}, apperror.WrapInternal(err)
	}
	page.NextCursor = &nextCursor
	return page, nil
}

func (service *Service) MarkRead(ctx context.Context, recipientID, notificationID uint64) error {
	if recipientID == 0 || notificationID == 0 {
		return validationError("notificationId must be a positive integer")
	}
	err := service.store.MarkRead(ctx, recipientID, notificationID)
	if errors.Is(err, ErrNotFound) {
		return apperror.New(apperror.CodeNotificationNotFound, "notification not found")
	}
	if err != nil {
		return apperror.WrapInternal(err)
	}
	return nil
}
