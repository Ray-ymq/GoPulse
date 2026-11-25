package post

import (
	"context"
	"errors"

	"github.com/Ray-ymq/GoPulse/backend/internal/shared/apperror"
)

func (s *Service) Delete(ctx context.Context, id, actor uint64) error {
	r, ok := s.repository.(interface {
		Delete(context.Context, uint64, uint64) error
	})
	if !ok {
		return apperror.WrapInternal(errors.New("post deletion unavailable"))
	}
	err := r.Delete(ctx, id, actor)
	if errors.Is(err, ErrNotFound) {
		return apperror.New(apperror.CodePostNotFound, "post not found")
	}
	if errors.Is(err, ErrPermissionDenied) {
		return apperror.New(apperror.CodePermissionDenied, "only the author may delete this post")
	}
	if err != nil {
		return apperror.WrapInternal(err)
	}
	return nil
}
