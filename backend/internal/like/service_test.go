package like

import (
	"context"
	"errors"
	"testing"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
)

type fakeRepository struct {
	create func(context.Context, uint64, uint64) error
	delete func(context.Context, uint64, uint64) error
	exists func(context.Context, uint64, uint64) (bool, error)
}

func (repository *fakeRepository) Create(ctx context.Context, postID, userID uint64) error {
	return repository.create(ctx, postID, userID)
}
func (repository *fakeRepository) Delete(ctx context.Context, postID, userID uint64) error {
	return repository.delete(ctx, postID, userID)
}
func (repository *fakeRepository) Exists(ctx context.Context, postID, userID uint64) (bool, error) {
	return repository.exists(ctx, postID, userID)
}

type fakePostExistence struct {
	require func(context.Context, uint64) error
}

func (posts *fakePostExistence) RequireExists(ctx context.Context, postID uint64) error {
	return posts.require(ctx, postID)
}

func TestServiceLikeTreatsOnlyDuplicateAsSuccess(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		code apperror.Code
	}{
		{name: "created"},
		{name: "already liked", err: ErrAlreadyExists},
		{name: "database failure", err: errors.New("foreign key or connection"), code: apperror.CodeInternal},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(&fakeRepository{
				create: func(_ context.Context, postID, userID uint64) error {
					if postID != 31 || userID != 17 {
						t.Fatalf("Create() post=%d user=%d", postID, userID)
					}
					return test.err
				},
			}, &fakePostExistence{require: func(context.Context, uint64) error { return nil }})
			err := service.Like(context.Background(), 31, 17)
			if test.code == "" && err != nil {
				t.Fatalf("Like() error=%v", err)
			}
			if test.code != "" {
				assertApplicationCode(t, err, test.code)
			}
		})
	}
}

func TestServiceLikeAndUnlikeRequireExistingPost(t *testing.T) {
	called := false
	missing := apperror.New(apperror.CodePostNotFound, "post not found")
	service := NewService(&fakeRepository{
		create: func(context.Context, uint64, uint64) error { called = true; return nil },
		delete: func(context.Context, uint64, uint64) error { called = true; return nil },
	}, &fakePostExistence{require: func(context.Context, uint64) error { return missing }})

	assertApplicationCode(t, service.Like(context.Background(), 31, 17), apperror.CodePostNotFound)
	assertApplicationCode(t, service.Unlike(context.Background(), 31, 17), apperror.CodePostNotFound)
	if called {
		t.Fatal("like repository called for missing post")
	}
}

func TestServiceUnlikeIsIdempotentAndMapsDatabaseFailure(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "deleted"},
		{name: "already absent"},
		{name: "database failure", err: errors.New("delete failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(&fakeRepository{
				delete: func(context.Context, uint64, uint64) error { return test.err },
			}, &fakePostExistence{require: func(context.Context, uint64) error { return nil }})
			err := service.Unlike(context.Background(), 31, 17)
			if test.err == nil && err != nil {
				t.Fatalf("Unlike() error=%v", err)
			}
			if test.err != nil {
				assertApplicationCode(t, err, apperror.CodeInternal)
			}
		})
	}
}

func TestServiceExistsProvidesViewerSpecificBoundary(t *testing.T) {
	service := NewService(&fakeRepository{exists: func(_ context.Context, postID, userID uint64) (bool, error) {
		if postID != 31 || userID != 17 {
			t.Fatalf("Exists() post=%d user=%d", postID, userID)
		}
		return true, nil
	}}, &fakePostExistence{})
	exists, err := service.Exists(context.Background(), 31, 17)
	if err != nil || !exists {
		t.Fatalf("Exists()=%t error=%v", exists, err)
	}

	failure := NewService(&fakeRepository{exists: func(context.Context, uint64, uint64) (bool, error) {
		return false, errors.New("query failed")
	}}, &fakePostExistence{})
	_, err = failure.Exists(context.Background(), 31, 17)
	assertApplicationCode(t, err, apperror.CodeInternal)
}

func assertApplicationCode(t *testing.T, err error, code apperror.Code) {
	t.Helper()
	applicationError, ok := apperror.As(err)
	if !ok || applicationError.Code != code {
		t.Fatalf("error=%#v, want code %q", err, code)
	}
}

type fakeDetailCacheInvalidator struct {
	invalidate func(context.Context, uint64) error
}

func (cache *fakeDetailCacheInvalidator) Invalidate(ctx context.Context, postID uint64) error {
	return cache.invalidate(ctx, postID)
}

func TestServiceLikeInvalidatesAfterCreatedOrIdempotentSuccess(t *testing.T) {
	for _, createError := range []error{nil, ErrAlreadyExists} {
		invalidations := 0
		service := NewService(
			&fakeRepository{create: func(context.Context, uint64, uint64) error { return createError }},
			&fakePostExistence{require: func(context.Context, uint64) error { return nil }},
			&fakeDetailCacheInvalidator{invalidate: func(_ context.Context, postID uint64) error {
				invalidations++
				if postID != 31 {
					t.Fatalf("Invalidate() post=%d", postID)
				}
				return errors.New("redis unavailable")
			}},
		)
		if err := service.Like(context.Background(), 31, 17); err != nil || invalidations != 1 {
			t.Fatalf("Like() error=%v invalidations=%d", err, invalidations)
		}
	}
}

func TestServiceUnlikeInvalidatesAfterIdempotentSuccess(t *testing.T) {
	invalidations := 0
	service := NewService(
		&fakeRepository{delete: func(context.Context, uint64, uint64) error { return nil }},
		&fakePostExistence{require: func(context.Context, uint64) error { return nil }},
		&fakeDetailCacheInvalidator{invalidate: func(context.Context, uint64) error {
			invalidations++
			return errors.New("redis unavailable")
		}},
	)
	if err := service.Unlike(context.Background(), 31, 17); err != nil || invalidations != 1 {
		t.Fatalf("Unlike() error=%v invalidations=%d", err, invalidations)
	}
}

func TestServiceLikeAndUnlikeDoNotInvalidateFailedMySQLWrites(t *testing.T) {
	invalidations := 0
	cache := &fakeDetailCacheInvalidator{invalidate: func(context.Context, uint64) error {
		invalidations++
		return nil
	}}
	posts := &fakePostExistence{require: func(context.Context, uint64) error { return nil }}
	likeService := NewService(&fakeRepository{create: func(context.Context, uint64, uint64) error {
		return errors.New("create failed")
	}}, posts, cache)
	assertApplicationCode(t, likeService.Like(context.Background(), 31, 17), apperror.CodeInternal)

	unlikeService := NewService(&fakeRepository{delete: func(context.Context, uint64, uint64) error {
		return errors.New("delete failed")
	}}, posts, cache)
	assertApplicationCode(t, unlikeService.Unlike(context.Background(), 31, 17), apperror.CodeInternal)
	if invalidations != 0 {
		t.Fatalf("invalidations=%d, want 0", invalidations)
	}
}
