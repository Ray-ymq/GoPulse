package comment

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
)

type fakeRepository struct {
	create func(context.Context, uint64, uint64, string) (Comment, error)
	list   func(context.Context, uint64, ListOptions) ([]Comment, error)
}

func (repository *fakeRepository) Create(ctx context.Context, postID, authorID uint64, content string) (Comment, error) {
	if repository.create == nil {
		return Comment{}, errors.New("unexpected Create call")
	}
	return repository.create(ctx, postID, authorID, content)
}

func (repository *fakeRepository) List(ctx context.Context, postID uint64, options ListOptions) ([]Comment, error) {
	if repository.list == nil {
		return nil, errors.New("unexpected List call")
	}
	return repository.list(ctx, postID, options)
}

type fakePostExistence struct {
	require func(context.Context, uint64) error
}

func (posts *fakePostExistence) RequireExists(ctx context.Context, postID uint64) error {
	if posts.require == nil {
		return errors.New("unexpected RequireExists call")
	}
	return posts.require(ctx, postID)
}

func TestServiceCreateUsesAuthenticatedAuthorAndNormalizedContent(t *testing.T) {
	record := Comment{ID: 8, PostID: 31, Content: "评论🙂", Author: Author{ID: 17, Username: "alice"}}
	service := NewService(
		&fakeRepository{create: func(_ context.Context, postID, authorID uint64, content string) (Comment, error) {
			if postID != 31 || authorID != 17 || content != "评论🙂" {
				t.Fatalf("Create() post=%d author=%d content=%q", postID, authorID, content)
			}
			return record, nil
		}},
		&fakePostExistence{require: func(_ context.Context, postID uint64) error {
			if postID != 31 {
				t.Fatalf("RequireExists() post=%d", postID)
			}
			return nil
		}},
	)

	got, err := service.Create(context.Background(), 31, 17, CreateInput{Content: " \n评论🙂\u3000"})
	if err != nil || got != record {
		t.Fatalf("Create() comment=%#v error=%v", got, err)
	}
}

func TestServiceCreateRejectsInvalidContentAndMapsFailures(t *testing.T) {
	called := false
	service := NewService(
		&fakeRepository{create: func(context.Context, uint64, uint64, string) (Comment, error) {
			called = true
			return Comment{}, errors.New("database secret")
		}},
		&fakePostExistence{require: func(context.Context, uint64) error { return nil }},
	)
	_, err := service.Create(context.Background(), 31, 17, CreateInput{Content: " "})
	assertApplicationCode(t, err, apperror.CodeValidationFailed)
	if called {
		t.Fatal("repository Create called for invalid content")
	}
	_, err = service.Create(context.Background(), 31, 17, CreateInput{Content: "valid"})
	assertApplicationCode(t, err, apperror.CodeInternal)

	missing := NewService(&fakeRepository{}, &fakePostExistence{require: func(context.Context, uint64) error {
		return apperror.New(apperror.CodePostNotFound, "post not found")
	}})
	_, err = missing.Create(context.Background(), 31, 17, CreateInput{Content: "valid"})
	assertApplicationCode(t, err, apperror.CodePostNotFound)
}

func TestServiceListBuildsStableCursorAndChecksPost(t *testing.T) {
	createdAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	records := []Comment{{ID: 5, CreatedAt: createdAt}, {ID: 4, CreatedAt: createdAt}, {ID: 3, CreatedAt: createdAt}}
	service := NewService(
		&fakeRepository{list: func(_ context.Context, postID uint64, options ListOptions) ([]Comment, error) {
			if postID != 31 || options.Limit != 2 || options.Cursor != nil {
				t.Fatalf("List() post=%d options=%#v", postID, options)
			}
			return records, nil
		}},
		&fakePostExistence{require: func(context.Context, uint64) error { return nil }},
	)
	page, err := service.List(context.Background(), 31, ListOptions{Limit: 2})
	if err != nil || len(page.Comments) != 2 || page.NextCursor == nil {
		t.Fatalf("List() page=%#v error=%v", page, err)
	}
	cursor, err := DecodeCursor(*page.NextCursor)
	if err != nil || cursor.ID != 4 {
		t.Fatalf("next cursor=%#v error=%v", cursor, err)
	}
}

func TestServiceListRejectsInvalidOptionsAndMapsFailures(t *testing.T) {
	called := false
	service := NewService(
		&fakeRepository{list: func(context.Context, uint64, ListOptions) ([]Comment, error) {
			called = true
			return nil, errors.New("database secret")
		}},
		&fakePostExistence{require: func(context.Context, uint64) error { return nil }},
	)
	for _, options := range []ListOptions{{Limit: 0}, {Limit: 51}, {Limit: 1, Cursor: &Cursor{}}} {
		_, err := service.List(context.Background(), 31, options)
		assertApplicationCode(t, err, apperror.CodeValidationFailed)
	}
	if called {
		t.Fatal("repository List called for invalid options")
	}
	_, err := service.List(context.Background(), 31, ListOptions{Limit: 20})
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

func TestServiceCreateInvalidatesPostDetailAfterMySQLSuccess(t *testing.T) {
	for _, invalidateError := range []error{nil, errors.New("redis unavailable")} {
		invalidations := 0
		service := NewService(
			&fakeRepository{create: func(context.Context, uint64, uint64, string) (Comment, error) {
				return Comment{ID: 8, PostID: 31}, nil
			}},
			&fakePostExistence{require: func(context.Context, uint64) error { return nil }},
			&fakeDetailCacheInvalidator{invalidate: func(_ context.Context, postID uint64) error {
				invalidations++
				if postID != 31 {
					t.Fatalf("Invalidate() post=%d", postID)
				}
				return invalidateError
			}},
		)
		record, err := service.Create(context.Background(), 31, 17, CreateInput{Content: "valid"})
		if err != nil || record.ID != 8 || invalidations != 1 {
			t.Fatalf("Create() record=%#v error=%v invalidations=%d", record, err, invalidations)
		}
	}
}

func TestServiceCreateDoesNotInvalidateWhenMySQLWriteFails(t *testing.T) {
	invalidations := 0
	service := NewService(
		&fakeRepository{create: func(context.Context, uint64, uint64, string) (Comment, error) {
			return Comment{}, errors.New("write failed")
		}},
		&fakePostExistence{require: func(context.Context, uint64) error { return nil }},
		&fakeDetailCacheInvalidator{invalidate: func(context.Context, uint64) error {
			invalidations++
			return nil
		}},
	)
	_, err := service.Create(context.Background(), 31, 17, CreateInput{Content: "valid"})
	assertApplicationCode(t, err, apperror.CodeInternal)
	if invalidations != 0 {
		t.Fatalf("invalidations=%d, want 0", invalidations)
	}
}
