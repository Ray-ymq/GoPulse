package post

import (
	"context"
	"errors"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (service *Service) Create(ctx context.Context, authorID uint64, input CreateInput) (Post, error) {
	normalized, err := NormalizeCreateInput(input)
	if errors.Is(err, ErrInvalidTitle) {
		return Post{}, validationError("title must contain between 1 and 120 characters")
	}
	if errors.Is(err, ErrInvalidContent) {
		return Post{}, validationError("content must contain between 1 and 10000 characters")
	}
	if err != nil {
		return Post{}, apperror.WrapInternal(err)
	}

	record, err := service.repository.Create(ctx, authorID, normalized.Title, normalized.Content)
	if err != nil {
		return Post{}, apperror.WrapInternal(err)
	}
	return record, nil
}

func (service *Service) List(ctx context.Context, viewerID uint64, options ListOptions) (Page, error) {
	if options.Limit < 1 || options.Limit > MaximumLimit {
		return Page{}, validationError("limit must be an integer between 1 and 50")
	}
	if options.Cursor != nil && (options.Cursor.ID == 0 || options.Cursor.CreatedAt.IsZero()) {
		return Page{}, validationError("cursor is invalid")
	}
	records, err := service.repository.List(ctx, viewerID, options)
	if err != nil {
		return Page{}, apperror.WrapInternal(err)
	}

	page := Page{Posts: records}
	if len(records) <= options.Limit {
		return page, nil
	}

	page.Posts = records[:options.Limit]
	last := page.Posts[len(page.Posts)-1]
	nextCursor, err := EncodeCursor(Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
	if err != nil {
		return Page{}, apperror.WrapInternal(err)
	}
	page.NextCursor = &nextCursor
	return page, nil
}

func (service *Service) Detail(ctx context.Context, postID, viewerID uint64) (Post, error) {
	record, err := service.repository.FindByID(ctx, postID, viewerID)
	if errors.Is(err, ErrNotFound) {
		return Post{}, apperror.New(apperror.CodePostNotFound, "post not found")
	}
	if err != nil {
		return Post{}, apperror.WrapInternal(err)
	}
	return record, nil
}
