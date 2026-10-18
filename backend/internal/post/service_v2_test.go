package post

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
)

type v2Repository struct {
	fakeRepository
	readContext func(context.Context, uint64) (ReadContext, error)
	findBody    func(context.Context, BodyKey) (Body, error)
	dynamic     func(context.Context, uint64, uint64) (DynamicProjection, error)
}

func (repository *v2Repository) ReadContext(ctx context.Context, postID uint64) (ReadContext, error) {
	return repository.readContext(ctx, postID)
}

func (repository *v2Repository) FindBody(ctx context.Context, key BodyKey) (Body, error) {
	return repository.findBody(ctx, key)
}

func (repository *v2Repository) FindDynamic(ctx context.Context, postID, authorID uint64) (DynamicProjection, error) {
	return repository.dynamic(ctx, postID, authorID)
}

type v2BodyCache struct {
	getError error
	body     Body
	present  bool
	getCalls int
	setCalls int
	set      func(BodyKey, Body) error
}

func (cache *v2BodyCache) GetBody(_ context.Context, key BodyKey) (Body, bool, error) {
	cache.getCalls++
	if cache.getError != nil {
		return Body{}, false, cache.getError
	}
	if cache.present && cache.body.ValidForKey(key) {
		return cache.body, true, nil
	}
	return Body{}, false, nil
}

func (cache *v2BodyCache) SetBody(_ context.Context, key BodyKey, body Body) error {
	cache.setCalls++
	if cache.set != nil {
		if err := cache.set(key, body); err != nil {
			return err
		}
	}
	cache.body, cache.present = body, true
	return nil
}

func TestServiceV2FixesBodyKeyAndReadsDynamicFactsPerRequest(t *testing.T) {
	created := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	key := BodyKey{Epoch: "epoch-a", PostID: 31, ContentRevision: 2}
	body := Body{ID: 31, AuthorID: 7, ContentRevision: 2, Title: "title", Content: "content", CreatedAt: created, UpdatedAt: created}
	dynamicCalls := 0
	originCalls := 0
	liked := false
	repository := &v2Repository{
		fakeRepository: fakeRepository{likedByViewer: func(context.Context, uint64, uint64) (bool, error) { return liked, nil }},
		readContext: func(_ context.Context, postID uint64) (ReadContext, error) {
			if postID != key.PostID {
				t.Fatalf("ReadContext() post=%d", postID)
			}
			return ReadContext{Epoch: key.Epoch, PostID: key.PostID, ContentRevision: key.ContentRevision, AuthorID: body.AuthorID, Visible: true}, nil
		},
		findBody: func(_ context.Context, got BodyKey) (Body, error) {
			originCalls++
			if got != key {
				t.Fatalf("FindBody() key=%#v", got)
			}
			return body, nil
		},
		dynamic: func(_ context.Context, postID, authorID uint64) (DynamicProjection, error) {
			dynamicCalls++
			if postID != key.PostID || authorID != body.AuthorID {
				t.Fatalf("FindDynamic() post=%d author=%d", postID, authorID)
			}
			return DynamicProjection{Author: PublicAuthor{ID: 7, Username: "alice"}, CommentCount: uint64(dynamicCalls), LikeCount: 5}, nil
		},
	}
	cache := &v2BodyCache{}
	service := NewService(repository).WithBodyCache(cache, NewRebuildCoordinator(context.Background()))
	first, err := service.Detail(context.Background(), key.PostID, 17)
	if err != nil || first.CommentCount != 1 || originCalls != 1 || cache.setCalls != 1 {
		t.Fatalf("first=%#v error=%v origin=%d set=%d", first, err, originCalls, cache.setCalls)
	}
	liked = true
	second, err := service.Detail(context.Background(), key.PostID, 17)
	if err != nil || second.CommentCount != 2 || !second.LikedByMe || originCalls != 1 {
		t.Fatalf("second=%#v error=%v origin=%d", second, err, originCalls)
	}
	if dynamicCalls != 2 || cache.getCalls < 2 {
		t.Fatalf("dynamic calls=%d cache gets=%d", dynamicCalls, cache.getCalls)
	}
}

func TestServiceV2DoesNotServeBodyWhenAuthorContextChanges(t *testing.T) {
	key := BodyKey{Epoch: "epoch-a", PostID: 31, ContentRevision: 1}
	oldBody := Body{ID: 31, AuthorID: 7, ContentRevision: 1, Title: "old", Content: "old", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	newBody := oldBody
	newBody.AuthorID = 8
	newBody.Title = "new"
	repository := &v2Repository{
		fakeRepository: fakeRepository{likedByViewer: func(context.Context, uint64, uint64) (bool, error) { return false, nil }},
		readContext: func(context.Context, uint64) (ReadContext, error) {
			return ReadContext{Epoch: key.Epoch, PostID: key.PostID, ContentRevision: 1, AuthorID: 8, Visible: true}, nil
		},
		findBody: func(_ context.Context, got BodyKey) (Body, error) {
			if got != key {
				return Body{}, errors.New("wrong key")
			}
			return newBody, nil
		},
		dynamic: func(context.Context, uint64, uint64) (DynamicProjection, error) {
			return DynamicProjection{Author: PublicAuthor{ID: 8, Username: "new"}}, nil
		},
	}
	cache := &v2BodyCache{body: oldBody, present: true}
	service := NewService(repository).WithBodyCache(cache, NewRebuildCoordinator(context.Background()))
	got, err := service.Detail(context.Background(), key.PostID, 17)
	if err != nil || got.Author.ID != 8 || got.Title != "new" {
		t.Fatalf("detail=%#v error=%v", got, err)
	}
	if cache.setCalls != 1 {
		t.Fatalf("set calls=%d, want replacement", cache.setCalls)
	}
}

func TestServiceV2ReadDuringEditDeleteAndRecoveryReturnsConsistentFollowUp(t *testing.T) {
	for _, test := range []struct {
		name         string
		oldKey       BodyKey
		newBody      Body
		mutate       func(*v2ReadMutation)
		firstCode    apperror.Code
		wantTitle    string
		wantRevision uint64
	}{
		{
			name:         "edit",
			oldKey:       BodyKey{Epoch: "epoch-a", PostID: 31, ContentRevision: 1},
			newBody:      serviceV2TestBody(31, 7, 2, "edited"),
			mutate:       func(state *v2ReadMutation) { state.revision = 2 },
			firstCode:    apperror.CodePostReadUnavailable,
			wantTitle:    "edited",
			wantRevision: 2,
		},
		{
			name:      "delete",
			oldKey:    BodyKey{Epoch: "epoch-a", PostID: 31, ContentRevision: 1},
			newBody:   serviceV2TestBody(31, 7, 1, "never served"),
			mutate:    func(state *v2ReadMutation) { state.visible = false },
			firstCode: apperror.CodePostReadUnavailable,
		},
		{
			name:         "epoch recovery",
			oldKey:       BodyKey{Epoch: "epoch-a", PostID: 31, ContentRevision: 1},
			newBody:      serviceV2TestBody(31, 7, 1, "recovered"),
			mutate:       func(state *v2ReadMutation) { state.epoch = "epoch-b" },
			firstCode:    apperror.CodePostReadUnavailable,
			wantTitle:    "recovered",
			wantRevision: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := &v2ReadMutation{epoch: test.oldKey.Epoch, revision: test.oldKey.ContentRevision, visible: true}
			bodyStarted := make(chan struct{})
			releaseBody := make(chan struct{})
			var once sync.Once
			repository := &v2Repository{
				fakeRepository: fakeRepository{likedByViewer: func(context.Context, uint64, uint64) (bool, error) { return false, nil }},
				readContext: func(_ context.Context, postID uint64) (ReadContext, error) {
					state.mu.Lock()
					defer state.mu.Unlock()
					return ReadContext{Epoch: state.epoch, PostID: postID, ContentRevision: state.revision, AuthorID: 7, Visible: state.visible}, nil
				},
				findBody: func(_ context.Context, key BodyKey) (Body, error) {
					if key == test.oldKey {
						once.Do(func() { close(bodyStarted) })
						<-releaseBody
						state.mu.Lock()
						changed := state.epoch != test.oldKey.Epoch || state.revision != test.oldKey.ContentRevision || !state.visible
						state.mu.Unlock()
						if changed {
							return Body{}, ErrReadContextChanged
						}
					}
					return test.newBody, nil
				},
				dynamic: func(context.Context, uint64, uint64) (DynamicProjection, error) {
					return DynamicProjection{Author: PublicAuthor{ID: 7, Username: "author"}}, nil
				},
			}
			cache := &v2BodyCache{}
			service := NewService(repository).WithBodyCache(cache, NewRebuildCoordinator(context.Background()))
			firstResult := make(chan struct {
				post Post
				err  error
			}, 1)
			go func() {
				got, err := service.Detail(context.Background(), test.oldKey.PostID, 17)
				firstResult <- struct {
					post Post
					err  error
				}{got, err}
			}()
			<-bodyStarted
			state.mu.Lock()
			test.mutate(state)
			state.mu.Unlock()
			close(releaseBody)
			first := <-firstResult
			assertPostApplicationCode(t, first.err, test.firstCode)

			second, err := service.Detail(context.Background(), test.oldKey.PostID, 17)
			if test.wantTitle == "" {
				assertPostApplicationCode(t, err, apperror.CodePostNotFound)
				return
			}
			if err != nil || second.Title != test.wantTitle || second.ContentRevision != test.wantRevision {
				t.Fatalf("follow-up post=%#v error=%v", second, err)
			}
		})
	}
}

type v2ReadMutation struct {
	mu       sync.Mutex
	epoch    string
	revision uint64
	visible  bool
}

func serviceV2TestBody(postID, authorID, revision uint64, title string) Body {
	created := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	return Body{ID: postID, AuthorID: authorID, ContentRevision: revision, Title: title, Content: title + " content", CreatedAt: created, UpdatedAt: created}
}

func TestBodyCacheFailuresUseBoundedOriginAndAuthorityFailuresReject(t *testing.T) {
	failure := errors.New("dependency unavailable")
	for _, test := range []struct {
		name                                                        string
		contextErr, originErr, dynamicErr, likedErr, getErr, setErr error
		hidden, warm                                                bool
		code                                                        apperror.Code
	}{
		{name: "Redis read failure", getErr: failure},
		{name: "Redis fill failure", setErr: failure},
		{name: "authoritative context failure with warm body", contextErr: failure, warm: true, code: apperror.CodePostReadUnavailable},
		{name: "deleted with warm body", contextErr: ErrNotFound, warm: true, code: apperror.CodePostNotFound},
		{name: "hidden with warm body", hidden: true, warm: true, code: apperror.CodePostNotFound},
		{name: "revision changed during origin", originErr: ErrReadContextChanged, code: apperror.CodePostReadUnavailable},
		{name: "origin failure", originErr: failure, code: apperror.CodePostReadUnavailable},
		{name: "dynamic facts failure with warm body", dynamicErr: failure, warm: true, code: apperror.CodePostReadUnavailable},
		{name: "viewer facts failure with warm body", likedErr: failure, warm: true, code: apperror.CodePostReadUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now().UTC()
			body := Body{ID: 31, AuthorID: 7, ContentRevision: 1, Title: "title", Content: "content", CreatedAt: now, UpdatedAt: now}
			origins := 0
			repository := &v2Repository{
				fakeRepository: fakeRepository{likedByViewer: func(context.Context, uint64, uint64) (bool, error) { return true, test.likedErr }},
				readContext: func(context.Context, uint64) (ReadContext, error) {
					return ReadContext{Epoch: "epoch-a", PostID: 31, ContentRevision: 1, AuthorID: 7, Visible: !test.hidden}, test.contextErr
				},
				findBody: func(context.Context, BodyKey) (Body, error) { origins++; return body, test.originErr },
				dynamic: func(context.Context, uint64, uint64) (DynamicProjection, error) {
					return DynamicProjection{Author: PublicAuthor{ID: 7, Username: "alice"}, LikeCount: 2}, test.dynamicErr
				},
			}
			cache := &v2BodyCache{body: body, present: test.warm, getError: test.getErr, set: func(BodyKey, Body) error { return test.setErr }}
			service := NewService(repository).WithBodyCache(cache, nil)
			t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
			got, err := service.Detail(context.Background(), 31, 17)
			if test.code != "" {
				assertPostApplicationCode(t, err, test.code)
			} else if err != nil || got.Content != "content" || !got.LikedByMe || got.LikeCount != 2 {
				t.Fatalf("detail=%+v error=%v", got, err)
			}
			if (test.contextErr != nil || test.hidden) && (cache.getCalls != 0 || cache.setCalls != 0 || origins != 0) {
				t.Fatalf("unverified read touched cache/origin: gets=%d sets=%d origins=%d", cache.getCalls, cache.setCalls, origins)
			}
			if test.originErr != nil && cache.setCalls != 0 {
				t.Fatal("failed origin was cached")
			}
		})
	}
}
