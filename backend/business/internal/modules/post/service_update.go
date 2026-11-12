package post

import (
	"context"
	"errors"

	"github.com/Ray-ymq/GoPulse/backend/internal/shared/apperror"
)

func (s *Service) Update(ctx context.Context, id, actor uint64, input CreateInput) (Post, error) {
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
	r, ok := s.repository.(interface {
		Update(context.Context, uint64, uint64, CreateInput) (Post, error)
	})
	if !ok {
		return Post{}, apperror.WrapInternal(errors.New("post editing unavailable"))
	}
	record, err := r.Update(ctx, id, actor, normalized)
	if errors.Is(err, ErrNotFound) {
		return Post{}, apperror.New(apperror.CodePostNotFound, "post not found")
	}
	if errors.Is(err, ErrPermissionDenied) {
		return Post{}, apperror.New(apperror.CodePermissionDenied, "only the author may edit this post")
	}
	if err != nil {
		return Post{}, apperror.WrapInternal(err)
	}
	return record, nil
}
