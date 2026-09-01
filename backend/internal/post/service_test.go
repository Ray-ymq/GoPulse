package post

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
)

type fakeRepository struct {
	create   func(context.Context, uint64, string, string) (Post, error)
	list     func(context.Context, uint64, ListOptions) ([]Post, error)
	findByID func(context.Context, uint64, uint64) (Post, error)
}

func (repository *fakeRepository) Create(ctx context.Context, authorID uint64, title, content string) (Post, error) {
	if repository.create == nil {
		return Post{}, errors.New("unexpected Create call")
	}
	return repository.create(ctx, authorID, title, content)
}

func (repository *fakeRepository) List(ctx context.Context, viewerID uint64, options ListOptions) ([]Post, error) {
	if repository.list == nil {
		return nil, errors.New("unexpected List call")
	}
	return repository.list(ctx, viewerID, options)
}

func (repository *fakeRepository) FindByID(ctx context.Context, postID, viewerID uint64) (Post, error) {
	if repository.findByID == nil {
		return Post{}, errors.New("unexpected FindByID call")
	}
	return repository.findByID(ctx, postID, viewerID)
}

func TestServiceCreateUsesAuthenticatedAuthorAndNormalizedInput(t *testing.T) {
	created := Post{ID: 8, Author: Author{ID: 17, Username: "alice"}}
	service := NewService(&fakeRepository{create: func(_ context.Context, authorID uint64, title, content string) (Post, error) {
		if authorID != 17 || title != "标题" || content != "content" {
			t.Fatalf("Create() arguments author=%d title=%q content=%q", authorID, title, content)
		}
		return created, nil
	}})

	got, err := service.Create(context.Background(), 17, CreateInput{Title: "  标题\u3000", Content: "\ncontent\t"})
	if err != nil || got.ID != created.ID || got.Author.ID != 17 {
		t.Fatalf("Create() post=%#v error=%v", got, err)
	}
}

func TestServiceCreateMapsValidationAndRepositoryErrors(t *testing.T) {
	called := false
	service := NewService(&fakeRepository{create: func(context.Context, uint64, string, string) (Post, error) {
		called = true
		return Post{}, errors.New("database secret")
	}})

	_, err := service.Create(context.Background(), 17, CreateInput{Title: " ", Content: "content"})
	assertPostApplicationCode(t, err, apperror.CodeValidationFailed)
	if called {
		t.Fatal("repository Create called for invalid input")
	}
	_, err = service.Create(context.Background(), 17, CreateInput{Title: "title", Content: "content"})
	assertPostApplicationCode(t, err, apperror.CodeInternal)
}

func TestServiceListBuildsNextCursorOnlyWhenExtraRecordExists(t *testing.T) {
	createdAt := time.Date(2026, 9, 1, 12, 0, 0, 123456000, time.UTC)
	for _, test := range []struct {
		name       string
		records    []Post
		wantLength int
		wantCursor bool
	}{
		{name: "empty", records: []Post{}, wantLength: 0},
		{name: "exact page", records: []Post{{ID: 3, CreatedAt: createdAt}, {ID: 2, CreatedAt: createdAt}}, wantLength: 2},
		{name: "extra record", records: []Post{{ID: 3, CreatedAt: createdAt}, {ID: 2, CreatedAt: createdAt}, {ID: 1, CreatedAt: createdAt}}, wantLength: 2, wantCursor: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(&fakeRepository{list: func(_ context.Context, viewerID uint64, options ListOptions) ([]Post, error) {
				if viewerID != 17 || options.Limit != 2 {
					t.Fatalf("List() viewer=%d options=%#v", viewerID, options)
				}
				return test.records, nil
			}})
			page, err := service.List(context.Background(), 17, ListOptions{Limit: 2})
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if len(page.Posts) != test.wantLength || (page.NextCursor != nil) != test.wantCursor {
				t.Fatalf("page = %#v", page)
			}
			if page.NextCursor != nil {
				cursor, err := DecodeCursor(*page.NextCursor)
				if err != nil || cursor.ID != 2 || !cursor.CreatedAt.Equal(createdAt) {
					t.Fatalf("next cursor = %#v error=%v", cursor, err)
				}
			}
		})
	}
}

func TestServiceListRejectsInvalidInternalOptionsBeforeRepository(t *testing.T) {
	called := false
	service := NewService(&fakeRepository{list: func(context.Context, uint64, ListOptions) ([]Post, error) {
		called = true
		return nil, nil
	}})
	for _, options := range []ListOptions{
		{Limit: 0},
		{Limit: MaximumLimit + 1},
		{Limit: 1, Cursor: &Cursor{ID: 1}},
		{Limit: 1, Cursor: &Cursor{CreatedAt: time.Now()}},
	} {
		_, err := service.List(context.Background(), 17, options)
		assertPostApplicationCode(t, err, apperror.CodeValidationFailed)
	}
	if called {
		t.Fatal("repository List called for invalid options")
	}
}

func TestServiceDetailMapsNotFoundAndInternalErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		code apperror.Code
	}{
		{name: "not found", err: ErrNotFound, code: apperror.CodePostNotFound},
		{name: "database", err: errors.New("sql detail"), code: apperror.CodeInternal},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(&fakeRepository{findByID: func(_ context.Context, postID, viewerID uint64) (Post, error) {
				if postID != 9 || viewerID != 17 {
					t.Fatalf("FindByID() post=%d viewer=%d", postID, viewerID)
				}
				return Post{}, test.err
			}})
			_, err := service.Detail(context.Background(), 9, 17)
			assertPostApplicationCode(t, err, test.code)
		})
	}
}

func assertPostApplicationCode(t *testing.T, err error, code apperror.Code) {
	t.Helper()
	appError, ok := apperror.As(err)
	if !ok || appError.Code != code {
		t.Fatalf("error = %#v, want application code %q", err, code)
	}
}
