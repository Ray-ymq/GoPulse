package post

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
)

type Service struct {
	repository Repository
	cache      DetailCache
	bodyCache  BodyCache
	rebuild    *RebuildCoordinator
	logger     *slog.Logger
}

// NewService creates the post application service. Passing one cache enables
// cache-aside only for detail reads; omitting it keeps the MySQL-only behavior.
func NewService(repository Repository, caches ...DetailCache) *Service {
	service := &Service{repository: repository, logger: logging.Module(logging.Discard("backend"), "cache")}
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

// WithBodyCache enables the closed-by-default v2 path. The legacy DetailCache
// remains attached separately so mixed v1/v2 instances can continue to expire
// old detail entries during the compatibility window.
func (service *Service) WithBodyCache(cache BodyCache, coordinator *RebuildCoordinator) *Service {
	if service == nil || cache == nil {
		return service
	}
	service.bodyCache = cache
	if coordinator == nil {
		coordinator = NewRebuildCoordinator(context.Background())
	}
	service.rebuild = coordinator
	return service
}

// Shutdown drains shared body rebuilds before the process changes data epoch.
func (service *Service) Shutdown(ctx context.Context) error {
	if service == nil || service.rebuild == nil {
		return nil
	}
	return service.rebuild.Stop(ctx)
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
	createdAt := last.CreatedAt
	if options.Bookmarks {
		createdAt = last.BookmarkCreatedAt
	}
	nextCursor, err := EncodeCursor(Cursor{CreatedAt: createdAt, ID: last.ID})
	if err != nil {
		return Page{}, apperror.WrapInternal(err)
	}
	page.NextCursor = &nextCursor
	return page, nil
}

func (service *Service) Detail(ctx context.Context, postID, viewerID uint64) (Post, error) {
	if service.bodyCache != nil {
		if reader, ok := service.repository.(ReadContextRepository); ok {
			return service.detailV2(ctx, postID, viewerID, reader)
		}
	}
	return service.detailV1(ctx, postID, viewerID)
}

func (service *Service) detailV1(ctx context.Context, postID, viewerID uint64) (Post, error) {
	projection, hit := service.cachedProjection(ctx, postID)
	if !hit {
		var err error
		projection, err = service.repository.FindPublicByID(ctx, postID)
		if errors.Is(err, ErrNotFound) {
			return Post{}, apperror.New(apperror.CodePostNotFound, "post not found")
		}
		if err != nil {
			return Post{}, apperror.WrapInternal(err)
		}
		if service.cache != nil {
			if err := service.cache.Set(ctx, projection); err != nil {
				logging.Module(logging.FromContext(ctx, service.logger), "cache").Warn("post detail cache fill failed", slog.Uint64("post_id", postID), slog.String("reason", "cache_unavailable"))
			}
		}
	}

	likedByMe, err := service.repository.LikedByViewer(ctx, postID, viewerID)
	if err != nil {
		return Post{}, apperror.WrapInternal(err)
	}
	records := []Post{projection.post(likedByMe)}
	if hydrator, ok := service.repository.(interface {
		HydrateAuthors(context.Context, []Post) error
	}); ok {
		if err := hydrator.HydrateAuthors(ctx, records); err != nil {
			return Post{}, apperror.WrapInternal(err)
		}
	}
	if err := service.hydrateFollowing(ctx, records, viewerID); err != nil {
		return Post{}, apperror.WrapInternal(err)
	}
	if r, ok := service.repository.(interface {
		HydrateBookmarks(context.Context, []Post, uint64) error
	}); ok {
		if err := r.HydrateBookmarks(ctx, records, viewerID); err != nil {
			return Post{}, apperror.WrapInternal(err)
		}
	}
	return records[0], nil
}

func (service *Service) detailV2(ctx context.Context, postID, viewerID uint64, reader ReadContextRepository) (Post, error) {
	readContext, err := reader.ReadContext(ctx, postID)
	if errors.Is(err, ErrNotFound) {
		return Post{}, apperror.New(apperror.CodePostNotFound, "post not found")
	}
	if err != nil {
		return Post{}, apperror.New(apperror.CodePostReadUnavailable, "post read temporarily unavailable")
	}
	if !readContext.Visible {
		return Post{}, apperror.New(apperror.CodePostNotFound, "post not found")
	}
	key := readContext.BodyKey()
	body, hit, cacheErr := service.bodyCache.GetBody(ctx, key)
	if cacheErr != nil {
		logging.Module(logging.FromContext(ctx, service.logger), "cache").Warn("post body cache read failed", slog.Uint64("post_id", postID), slog.String("reason", "cache_unavailable"))
		hit = false
	}
	if hit && (body.AuthorID != readContext.AuthorID || !body.ValidForKey(key)) {
		hit = false
	}
	if !hit {
		coordinator := service.rebuild
		if coordinator == nil {
			coordinator = NewRebuildCoordinator(context.Background())
		}
		body, err = coordinator.Load(ctx, key, func(loadContext context.Context, loadKey BodyKey) (Body, error) {
			// The executor rechecks the exact key after registration. A previous
			// waiter may have filled it while this request was joining.
			if cached, cachedHit, getErr := service.bodyCache.GetBody(loadContext, loadKey); getErr == nil && cachedHit && cached.AuthorID == readContext.AuthorID && cached.ValidForKey(loadKey) {
				return cached, nil
			}
			originStarted := time.Now()
			loaded, loadErr := reader.FindBody(loadContext, loadKey)
			if loadErr != nil {
				observeBodyOrigin(loadErr, time.Since(originStarted))
				return Body{}, loadErr
			}
			observeBodyOrigin(nil, time.Since(originStarted))
			if setErr := service.bodyCache.SetBody(loadContext, loadKey, loaded); setErr != nil {
				logging.Module(logging.FromContext(loadContext, service.logger), "cache").Warn("post body cache fill failed", slog.Uint64("post_id", postID), slog.String("reason", "cache_unavailable"))
			}
			return loaded, nil
		})
		if errors.Is(err, ErrNotFound) {
			return Post{}, apperror.New(apperror.CodePostNotFound, "post not found")
		}
		if errors.Is(err, ErrRebuildBusy) {
			return Post{}, apperror.New(apperror.CodeBackendBusy, "service temporarily busy")
		}
		if errors.Is(err, ErrReadContextChanged) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return Post{}, apperror.New(apperror.CodePostReadUnavailable, "post read temporarily unavailable")
		}
		if err != nil {
			return Post{}, apperror.New(apperror.CodePostReadUnavailable, "post read temporarily unavailable")
		}
	}
	dynamic, err := reader.FindDynamic(ctx, postID, readContext.AuthorID)
	if errors.Is(err, ErrNotFound) {
		return Post{}, apperror.New(apperror.CodePostNotFound, "post not found")
	}
	if err != nil {
		return Post{}, apperror.New(apperror.CodePostReadUnavailable, "post read temporarily unavailable")
	}
	projection := PublicProjection{
		EditedAt: body.EditedAt, ContentRevision: body.ContentRevision, ID: body.ID,
		Title: body.Title, Content: body.Content, CreatedAt: body.CreatedAt, UpdatedAt: body.UpdatedAt,
		Author: dynamic.Author, CommentCount: dynamic.CommentCount, LikeCount: dynamic.LikeCount,
	}
	likedByMe, err := service.repository.LikedByViewer(ctx, postID, viewerID)
	if err != nil {
		return Post{}, apperror.New(apperror.CodePostReadUnavailable, "post read temporarily unavailable")
	}
	records := []Post{projection.post(likedByMe)}
	if err := service.hydrateFollowing(ctx, records, viewerID); err != nil {
		return Post{}, apperror.New(apperror.CodePostReadUnavailable, "post read temporarily unavailable")
	}
	if hydrator, ok := service.repository.(interface {
		HydrateBookmarks(context.Context, []Post, uint64) error
	}); ok {
		if err := hydrator.HydrateBookmarks(ctx, records, viewerID); err != nil {
			return Post{}, apperror.New(apperror.CodePostReadUnavailable, "post read temporarily unavailable")
		}
	}
	return records[0], nil
}

func observeBodyOrigin(err error, elapsed time.Duration) {
	if metrics := componentmetrics.BackendActive(); metrics != nil {
		result := "success"
		if errors.Is(err, ErrReadContextChanged) {
			result = "version_changed"
		} else if err != nil {
			result = "failure"
		}
		metrics.ObserveCacheOriginDuration(result, elapsed)
	}
}

func (service *Service) cachedProjection(ctx context.Context, postID uint64) (PublicProjection, bool) {
	if service.cache == nil {
		return PublicProjection{}, false
	}
	projection, hit, err := service.cache.Get(ctx, postID)
	if err != nil {
		logging.Module(logging.FromContext(ctx, service.logger), "cache").Warn("post detail cache read failed", slog.Uint64("post_id", postID), slog.String("reason", "cache_unavailable"))
		return PublicProjection{}, false
	}
	if hit {
		r, ok := service.repository.(interface {
			ContentRevision(context.Context, uint64) (uint64, error)
		})
		if !ok {
			return PublicProjection{}, false
		}
		revision, err := r.ContentRevision(ctx, postID)
		if err != nil || revision != projection.ContentRevision {
			_ = service.cache.Invalidate(ctx, postID)
			return PublicProjection{}, false
		}
	}
	return projection, hit
}

// RequireExists exposes the shared post-existence boundary used by dependent
// fact services without loading the complete viewer-specific post read model.
func (service *Service) RequireExists(ctx context.Context, postID uint64) error {
	exists, err := service.repository.Exists(ctx, postID)
	if err != nil {
		return apperror.WrapInternal(err)
	}
	if !exists {
		return apperror.New(apperror.CodePostNotFound, "post not found")
	}
	return nil
}

func (s *Service) hydrateFollowing(ctx context.Context, records []Post, viewer uint64) error {
	if r, ok := s.repository.(interface {
		HydrateFollowing(context.Context, []Post, uint64) error
	}); ok {
		return r.HydrateFollowing(ctx, records, viewer)
	}
	return nil
}
