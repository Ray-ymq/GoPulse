package bookmark

import (
	"context"
	"errors"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
)

type postExistence interface {
	RequireExists(context.Context, uint64) error
}

type Service struct {
	repository Repository
	posts      postExistence
}

func NewService(repository Repository, posts postExistence) *Service {
	return &Service{repository: repository, posts: posts}
}

// Bookmark ensures the authenticated user has exactly one bookmark fact for the post.
func (service *Service) Bookmark(ctx context.Context, postID, userID uint64) error {
	if err := service.posts.RequireExists(ctx, postID); err != nil {
		return err
	}
	if err := service.repository.Create(ctx, postID, userID); err != nil && !errors.Is(err, ErrAlreadyExists) {
		return bookmarkError(err)
	}
	return nil
}

// Unbookmark ensures the authenticated user has no bookmark fact for the post.
func (service *Service) Unbookmark(ctx context.Context, postID, userID uint64) error {
	if err := service.posts.RequireExists(ctx, postID); err != nil {
		return err
	}
	if err := service.repository.Delete(ctx, postID, userID); err != nil {
		return bookmarkError(err)
	}
	return nil
}

func bookmarkError(err error) error {
	if errors.Is(err, post.ErrNotFound) {
		return apperror.New(apperror.CodePostNotFound, "post not found")
	}
	return apperror.WrapInternal(err)
}
