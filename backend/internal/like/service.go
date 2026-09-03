package like

import (
	"context"
	"errors"
	"log/slog"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
)

type postExistence interface {
	RequireExists(context.Context, uint64) error
}

type detailCacheInvalidator interface {
	Invalidate(context.Context, uint64) error
}

type Service struct {
	repository Repository
	posts      postExistence
	cache      detailCacheInvalidator
	logger     *slog.Logger
}

func NewService(repository Repository, posts postExistence, caches ...detailCacheInvalidator) *Service {
	service := &Service{repository: repository, posts: posts, logger: logging.Module(logging.Discard("backend"), "cache")}
	if len(caches) > 0 {
		service.cache = caches[0]
	}
	return service
}

// WithLogger injects the process logger used for non-blocking cache warnings.
func (service *Service) WithLogger(logger *slog.Logger) *Service {
	if logger != nil {
		service.logger = logging.Module(logger, "cache")
	}
	return service
}

// Like ensures the authenticated user has exactly one like fact for the post.
func (service *Service) Like(ctx context.Context, postID, userID uint64) error {
	if err := service.posts.RequireExists(ctx, postID); err != nil {
		return err
	}
	if err := service.repository.Create(ctx, postID, userID); err != nil && !errors.Is(err, ErrAlreadyExists) {
		return apperror.WrapInternal(err)
	}
	service.invalidatePostDetail(ctx, postID, "like")
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
	service.invalidatePostDetail(ctx, postID, "unlike")
	return nil
}

func (service *Service) invalidatePostDetail(ctx context.Context, postID uint64, operation string) {
	if service.cache == nil {
		return
	}
	if err := service.cache.Invalidate(ctx, postID); err != nil {
		logging.Module(logging.FromContext(ctx, service.logger), "cache").Warn("post detail cache invalidation failed", slog.Uint64("post_id", postID), slog.String("reason", "cache_unavailable"), slog.String("operation", operation))
	}
}

// Exists provides the separate viewer-specific like boundary used by post read assembly.
func (service *Service) Exists(ctx context.Context, postID, userID uint64) (bool, error) {
	exists, err := service.repository.Exists(ctx, postID, userID)
	if err != nil {
		return false, apperror.WrapInternal(err)
	}
	return exists, nil
}
