package like

import (
	"context"
	"errors"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
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

// Like ensures the authenticated user has exactly one like fact for the post.
func (service *Service) Like(ctx context.Context, postID, userID uint64) error {
	if err := service.posts.RequireExists(ctx, postID); err != nil {
		return err
	}
	if err := service.repository.Create(ctx, postID, userID); err != nil {
		if errors.Is(err, ErrAlreadyExists) {
			return nil
		}
		return apperror.WrapInternal(err)
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
