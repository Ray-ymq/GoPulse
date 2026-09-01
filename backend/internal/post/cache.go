package post

import (
	"context"
	"strings"
	"time"
)

// PublicProjection is the non-personalized post detail payload eligible for
// caching. Viewer-specific state such as LikedByMe must never be stored here.
type PublicProjection struct {
	ID           uint64    `json:"id"`
	Title        string    `json:"title"`
	Content      string    `json:"content"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Author       Author    `json:"author"`
	CommentCount uint64    `json:"comment_count"`
	LikeCount    uint64    `json:"like_count"`
}

// DetailCache is the minimal cache-aside boundary used by post reads and
// interaction writes. Implementations must treat failures as non-authoritative.
type DetailCache interface {
	Get(context.Context, uint64) (PublicProjection, bool, error)
	Set(context.Context, PublicProjection) error
	Invalidate(context.Context, uint64) error
}

func (projection PublicProjection) post(likedByMe bool) Post {
	return Post{
		ID:           projection.ID,
		Title:        projection.Title,
		Content:      projection.Content,
		CreatedAt:    projection.CreatedAt,
		UpdatedAt:    projection.UpdatedAt,
		Author:       projection.Author,
		CommentCount: projection.CommentCount,
		LikeCount:    projection.LikeCount,
		LikedByMe:    likedByMe,
	}
}

// ValidForKey prevents malformed, incomplete, or cross-key cache values from
// being used as a business response.
func (projection PublicProjection) ValidForKey(postID uint64) bool {
	return projection.ID == postID &&
		projection.ID > 0 &&
		strings.TrimSpace(projection.Title) != "" &&
		strings.TrimSpace(projection.Content) != "" &&
		!projection.CreatedAt.IsZero() &&
		!projection.UpdatedAt.IsZero() &&
		projection.Author.ID > 0 &&
		strings.TrimSpace(projection.Author.Username) != ""
}
