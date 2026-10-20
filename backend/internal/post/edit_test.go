package post

import (
	"context"
	"testing"
)

type returningEditRepository struct {
	fakeRepository
	record Post
}

func (r *returningEditRepository) Update(_ context.Context, id, actor uint64, input CreateInput) (Post, error) {
	r.record.Title = input.Title
	r.record.Content = input.Content
	return r.record, nil
}
func TestEditReturnsCommittedProjectionWithoutDetailReads(t *testing.T) {
	repository := &returningEditRepository{record: Post{ID: 9, ContentRevision: 2, Author: Author{ID: 3, Username: "author"}, LikeCount: 4, CommentCount: 2, LikedByMe: true, BookmarkedByMe: true}}
	service := NewService(repository).WithBodyCache(&v2BodyCache{getError: ErrNotFound}, nil)
	defer service.Shutdown(context.Background())
	got, err := service.Update(context.Background(), 9, 3, CreateInput{Title: " edited ", Content: " body "})
	if err != nil || got.Title != "edited" || got.Content != "body" || got.LikeCount != 4 || !got.BookmarkedByMe || got.Author.Username != "author" {
		t.Fatalf("projection=%+v err=%v", got, err)
	}
}
