package post

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
)

type fakeRepository struct {
	create        func(context.Context, uint64, string, string) (Post, error)
	list          func(context.Context, uint64, ListOptions) ([]Post, error)
	findPublic    func(context.Context, uint64) (PublicProjection, error)
	likedByViewer func(context.Context, uint64, uint64) (bool, error)
	exists        func(context.Context, uint64) (bool, error)
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

func (repository *fakeRepository) FindPublicByID(ctx context.Context, postID uint64) (PublicProjection, error) {
	if repository.findPublic == nil {
		return PublicProjection{}, errors.New("unexpected FindPublicByID call")
	}
	return repository.findPublic(ctx, postID)
}

func (repository *fakeRepository) LikedByViewer(ctx context.Context, postID, viewerID uint64) (bool, error) {
	if repository.likedByViewer == nil {
		return false, errors.New("unexpected LikedByViewer call")
	}
	return repository.likedByViewer(ctx, postID, viewerID)
}

func (repository *fakeRepository) Exists(ctx context.Context, postID uint64) (bool, error) {
	if repository.exists == nil {
		return false, errors.New("unexpected Exists call")
	}
	return repository.exists(ctx, postID)
}

type fakeDetailCache struct {
	get        func(context.Context, uint64) (PublicProjection, bool, error)
	set        func(context.Context, PublicProjection) error
	invalidate func(context.Context, uint64) error
}

func (cache *fakeDetailCache) Get(ctx context.Context, postID uint64) (PublicProjection, bool, error) {
	if cache.get == nil {
		return PublicProjection{}, false, errors.New("unexpected cache Get call")
	}
	return cache.get(ctx, postID)
}

func (cache *fakeDetailCache) Set(ctx context.Context, projection PublicProjection) error {
	if cache.set == nil {
		return errors.New("unexpected cache Set call")
	}
	return cache.set(ctx, projection)
}

func (cache *fakeDetailCache) Invalidate(ctx context.Context, postID uint64) error {
	if cache.invalidate == nil {
		return errors.New("unexpected cache Invalidate call")
	}
	return cache.invalidate(ctx, postID)
}

func testProjection(commentCount, likeCount uint64) PublicProjection {
	createdAt := time.Date(2026, 9, 1, 12, 0, 0, 123456000, time.UTC)
	return PublicProjection{
		ID:           31,
		Title:        "cached title",
		Content:      "cached content",
		CreatedAt:    createdAt,
		UpdatedAt:    createdAt,
		Author:       Author{ID: 7, Username: "author"},
		CommentCount: commentCount,
		LikeCount:    likeCount,
	}
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

func TestServiceDetailCacheHitKeepsLikedByMeViewerSpecific(t *testing.T) {
	projection := testProjection(2, 1)
	cacheReads := 0
	publicReads := 0
	service := NewService(&fakeRepository{
		findPublic: func(context.Context, uint64) (PublicProjection, error) {
			publicReads++
			return PublicProjection{}, errors.New("unexpected public read")
		},
		likedByViewer: func(_ context.Context, postID, viewerID uint64) (bool, error) {
			if postID != 31 {
				t.Fatalf("LikedByViewer() post=%d", postID)
			}
			return viewerID == 17, nil
		},
	}, &fakeDetailCache{get: func(_ context.Context, postID uint64) (PublicProjection, bool, error) {
		cacheReads++
		if postID != 31 {
			t.Fatalf("cache Get() post=%d", postID)
		}
		return projection, true, nil
	}})

	first, err := service.Detail(context.Background(), 31, 17)
	if err != nil || !first.LikedByMe || first.CommentCount != 2 {
		t.Fatalf("first detail=%#v error=%v", first, err)
	}
	second, err := service.Detail(context.Background(), 31, 23)
	if err != nil || second.LikedByMe || second.LikeCount != 1 {
		t.Fatalf("second detail=%#v error=%v", second, err)
	}
	if cacheReads != 2 || publicReads != 0 {
		t.Fatalf("cache reads=%d public reads=%d", cacheReads, publicReads)
	}
}

func TestServiceDetailCacheMissReadsMySQLAndBestEffortFills(t *testing.T) {
	projection := testProjection(3, 4)
	var filled PublicProjection
	service := NewService(&fakeRepository{
		findPublic: func(_ context.Context, postID uint64) (PublicProjection, error) {
			if postID != 31 {
				t.Fatalf("FindPublicByID() post=%d", postID)
			}
			return projection, nil
		},
		likedByViewer: func(_ context.Context, postID, viewerID uint64) (bool, error) {
			return postID == 31 && viewerID == 17, nil
		},
	}, &fakeDetailCache{
		get: func(context.Context, uint64) (PublicProjection, bool, error) {
			return PublicProjection{}, false, nil
		},
		set: func(_ context.Context, got PublicProjection) error {
			filled = got
			return nil
		},
	})

	record, err := service.Detail(context.Background(), 31, 17)
	if err != nil || !record.LikedByMe || record.CommentCount != 3 || filled.ID != 31 {
		t.Fatalf("detail=%#v filled=%#v error=%v", record, filled, err)
	}
}

func TestServiceDetailCacheFailuresDegradeToMySQL(t *testing.T) {
	projection := testProjection(5, 6)
	for _, test := range []struct {
		name     string
		getError error
		setError error
	}{
		{name: "read failure", getError: errors.New("redis unavailable")},
		{name: "fill failure", setError: errors.New("redis unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(&fakeRepository{
				findPublic:    func(context.Context, uint64) (PublicProjection, error) { return projection, nil },
				likedByViewer: func(context.Context, uint64, uint64) (bool, error) { return true, nil },
			}, &fakeDetailCache{
				get: func(context.Context, uint64) (PublicProjection, bool, error) {
					return PublicProjection{}, false, test.getError
				},
				set: func(context.Context, PublicProjection) error { return test.setError },
			})
			record, err := service.Detail(context.Background(), 31, 17)
			if err != nil || record.CommentCount != 5 || !record.LikedByMe {
				t.Fatalf("detail=%#v error=%v", record, err)
			}
		})
	}
}

func TestServiceDetailMapsMySQLErrorsAndDoesNotNegativeCache(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		code apperror.Code
	}{
		{name: "not found", err: ErrNotFound, code: apperror.CodePostNotFound},
		{name: "database", err: errors.New("sql detail"), code: apperror.CodeInternal},
	} {
		t.Run(test.name, func(t *testing.T) {
			setCalls := 0
			likedCalls := 0
			service := NewService(&fakeRepository{
				findPublic: func(_ context.Context, postID uint64) (PublicProjection, error) {
					if postID != 9 {
						t.Fatalf("FindPublicByID() post=%d", postID)
					}
					return PublicProjection{}, test.err
				},
				likedByViewer: func(context.Context, uint64, uint64) (bool, error) {
					likedCalls++
					return false, nil
				},
			}, &fakeDetailCache{
				get: func(context.Context, uint64) (PublicProjection, bool, error) {
					return PublicProjection{}, false, nil
				},
				set: func(context.Context, PublicProjection) error {
					setCalls++
					return nil
				},
			})
			_, err := service.Detail(context.Background(), 9, 17)
			assertPostApplicationCode(t, err, test.code)
			if setCalls != 0 || likedCalls != 0 {
				t.Fatalf("set calls=%d liked calls=%d", setCalls, likedCalls)
			}
		})
	}
}

func TestServiceDetailTreatsViewerLikeQueryAsMySQLTruth(t *testing.T) {
	service := NewService(&fakeRepository{likedByViewer: func(context.Context, uint64, uint64) (bool, error) {
		return false, errors.New("mysql unavailable")
	}}, &fakeDetailCache{get: func(context.Context, uint64) (PublicProjection, bool, error) {
		return testProjection(2, 1), true, nil
	}})
	_, err := service.Detail(context.Background(), 31, 17)
	assertPostApplicationCode(t, err, apperror.CodeInternal)
}

func TestServiceDetailConcurrentOldReadCanRefillAfterInvalidationThenConverges(t *testing.T) {
	oldProjection := testProjection(0, 0)
	newProjection := testProjection(1, 1)
	var repositoryMu sync.RWMutex
	currentProjection := oldProjection
	liked := false
	repository := &fakeRepository{
		findPublic: func(context.Context, uint64) (PublicProjection, error) {
			repositoryMu.RLock()
			defer repositoryMu.RUnlock()
			return currentProjection, nil
		},
		likedByViewer: func(context.Context, uint64, uint64) (bool, error) {
			repositoryMu.RLock()
			defer repositoryMu.RUnlock()
			return liked, nil
		},
	}

	var cacheMu sync.Mutex
	var cached PublicProjection
	cachePresent := false
	setStarted := make(chan struct{})
	releaseSet := make(chan struct{})
	firstSet := true
	cache := &fakeDetailCache{
		get: func(context.Context, uint64) (PublicProjection, bool, error) {
			cacheMu.Lock()
			defer cacheMu.Unlock()
			return cached, cachePresent, nil
		},
		set: func(_ context.Context, projection PublicProjection) error {
			if firstSet {
				firstSet = false
				close(setStarted)
				<-releaseSet
			}
			cacheMu.Lock()
			cached = projection
			cachePresent = true
			cacheMu.Unlock()
			return nil
		},
		invalidate: func(context.Context, uint64) error {
			cacheMu.Lock()
			cachePresent = false
			cacheMu.Unlock()
			return nil
		},
	}
	service := NewService(repository, cache)

	firstResult := make(chan Post, 1)
	firstError := make(chan error, 1)
	go func() {
		record, err := service.Detail(context.Background(), 31, 17)
		firstResult <- record
		firstError <- err
	}()
	<-setStarted

	repositoryMu.Lock()
	currentProjection = newProjection
	liked = true
	repositoryMu.Unlock()
	if err := cache.Invalidate(context.Background(), 31); err != nil {
		t.Fatal(err)
	}
	close(releaseSet)
	if err := <-firstError; err != nil {
		t.Fatalf("first detail error=%v", err)
	}
	if first := <-firstResult; first.CommentCount != 0 || !first.LikedByMe {
		t.Fatalf("first detail=%#v", first)
	}

	stale, err := service.Detail(context.Background(), 31, 17)
	if err != nil || stale.CommentCount != 0 || stale.LikeCount != 0 || !stale.LikedByMe {
		t.Fatalf("stale detail=%#v error=%v", stale, err)
	}
	if err := cache.Invalidate(context.Background(), 31); err != nil {
		t.Fatal(err)
	}
	converged, err := service.Detail(context.Background(), 31, 17)
	if err != nil || converged.CommentCount != 1 || converged.LikeCount != 1 || !converged.LikedByMe {
		t.Fatalf("converged detail=%#v error=%v", converged, err)
	}
}

func assertPostApplicationCode(t *testing.T, err error, code apperror.Code) {
	t.Helper()
	appError, ok := apperror.As(err)
	if !ok || appError.Code != code {
		t.Fatalf("error = %#v, want application code %q", err, code)
	}
}

func TestServiceRequireExistsMapsMissingAndRepositoryErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		exists bool
		err    error
		code   apperror.Code
	}{
		{name: "exists", exists: true},
		{name: "missing", code: apperror.CodePostNotFound},
		{name: "database", err: errors.New("sql existence"), code: apperror.CodeInternal},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(&fakeRepository{exists: func(_ context.Context, postID uint64) (bool, error) {
				if postID != 31 {
					t.Fatalf("Exists() postID = %d", postID)
				}
				return test.exists, test.err
			}})
			err := service.RequireExists(context.Background(), 31)
			if test.code == "" {
				if err != nil {
					t.Fatalf("RequireExists() error = %v", err)
				}
				return
			}
			assertPostApplicationCode(t, err, test.code)
		})
	}
}
