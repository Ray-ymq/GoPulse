package like

import (
	"context"
	"errors"

	"github.com/Ray-ymq/GoPulse/backend/internal/shared/apperror"
)

type postExistence interface {
	RequireExists(context.Context, uint64) error
}

type Service struct {
	repository       Repository
	posts            postExistence
	invalidateCounts func(context.Context, uint64)
}

func NewService(repository Repository, posts postExistence) *Service {
	return &Service{repository: repository, posts: posts}
}

// Like ensures the authenticated user has exactly one like fact for the post.
func (service *Service) WithCountInvalidation(invalidate func(context.Context, uint64)) *Service {
	service.invalidateCounts = invalidate
	return service
}

func (service *Service) Like(ctx context.Context, postID, userID uint64) error {
	if err := service.posts.RequireExists(ctx, postID); err != nil {
		return err
	}
	err := service.repository.Create(ctx, postID, userID)
	if errors.Is(err, ErrAlreadyExists) {
		return nil
	}
	if err != nil {
		return apperror.WrapInternal(err)
	}
	if service.invalidateCounts != nil {
		service.invalidateCounts(ctx, postID)
	}
	return nil
}

// Unlike ensures the authenticated user has no like fact for the post.
func (service *Service) Unlike(ctx context.Context, postID, userID uint64) error {
	if err := service.posts.RequireExists(ctx, postID); err != nil {
		return err
	}
	if err := service.repository.Delete(ctx, postID, userID); err != nil {
		return apperror.WrapInternal(err)
	}
	if service.invalidateCounts != nil {
		service.invalidateCounts(ctx, postID)
	}
	return nil
}

// Exists provides the separate viewer-specific like boundary used by post read assembly.
func (service *Service) Exists(ctx context.Context, postID, userID uint64) (bool, error) {
	exists, err := service.repository.Exists(ctx, postID, userID)
	if err != nil {
		return false, apperror.WrapInternal(err)
	}
	return exists, nil
}
