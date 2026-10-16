package post

import (
	"context"
	"errors"
	"testing"
	"time"
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
	body     Body
	present  bool
	getCalls int
	setCalls int
	set      func(BodyKey, Body) error
}

func (cache *v2BodyCache) GetBody(_ context.Context, key BodyKey) (Body, bool, error) {
	cache.getCalls++
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
