package post

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
)

var ErrReadContextChanged = errors.New("post read context changed")

// BodyKey is the complete immutable identity of a public post body. Every
// cache read, origin load, and write-back in the v2 path carries this value;
// no adapter is allowed to resolve the current epoch implicitly.
type BodyKey struct {
	Epoch           string
	PostID          uint64
	ContentRevision uint64
}

func (key BodyKey) Valid() bool {
	return strings.TrimSpace(key.Epoch) != "" && key.PostID > 0 && key.ContentRevision > 0
}

func (key BodyKey) String() string {
	return key.Epoch + ":" + strconv.FormatUint(key.PostID, 10) + ":" + strconv.FormatUint(key.ContentRevision, 10)
}

// ReadContext is captured once from the authoritative database before a body
// cache key is chosen. Visibility is explicit so a deleted row cannot be
// reconstructed from a stale cache value.
type ReadContext struct {
	Epoch           string
	PostID          uint64
	ContentRevision uint64
	AuthorID        uint64
	Visible         bool
	ReadAt          time.Time
}

func (readContext ReadContext) BodyKey() BodyKey {
	return BodyKey{Epoch: readContext.Epoch, PostID: readContext.PostID, ContentRevision: readContext.ContentRevision}
}

// Body contains only public body data and immutable content metadata. Mutable
// author profile fields and all viewer-specific or interaction fields remain
// outside shared storage.
type Body struct {
	EditedAt        *time.Time `json:"edited_at"`
	ID              uint64     `json:"id"`
	AuthorID        uint64     `json:"author_id"`
	ContentRevision uint64     `json:"content_revision"`
	Title           string     `json:"title"`
	Content         string     `json:"content"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (body Body) ValidForKey(key BodyKey) bool {
	return key.Valid() && body.ID == key.PostID && body.ID > 0 && body.AuthorID > 0 && body.ContentRevision == key.ContentRevision &&
		strings.TrimSpace(body.Title) != "" && strings.TrimSpace(body.Content) != "" && !body.CreatedAt.IsZero() && !body.UpdatedAt.IsZero()
}

// BodyCache is the v2 cache-aside boundary. It intentionally does not expose
// invalidation by post ID: old versions must remain isolated by BodyKey.
type BodyCache interface {
	GetBody(context.Context, BodyKey) (Body, bool, error)
	SetBody(context.Context, BodyKey, Body) error
}

// ReadContextRepository supplies authoritative visibility and versioned body reads.
type ReadContextRepository interface {
	ReadContext(context.Context, uint64) (ReadContext, error)
	FindBody(context.Context, BodyKey) (Body, error)
	FindDynamic(context.Context, uint64, uint64) (DynamicProjection, error)
}

// DynamicProjection is intentionally separate from Body. Counts and author
// profile values are authoritative MySQL reads for every detail response.
type DynamicProjection struct {
	Author       PublicAuthor
	CommentCount uint64
	LikeCount    uint64
}

// PublicAuthor excludes relationship and permission fields from shared storage.
type PublicAuthor struct {
	ID          uint64 `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

// PublicProjection assembles public database fields for a detail response.
// Only Body is stored in the shared cache.
type PublicProjection struct {
	EditedAt        *time.Time   `json:"edited_at"`
	ContentRevision uint64       `json:"content_revision"`
	ID              uint64       `json:"id"`
	Title           string       `json:"title"`
	Content         string       `json:"content"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
	Author          PublicAuthor `json:"author"`
	CommentCount    uint64       `json:"comment_count"`
	LikeCount       uint64       `json:"like_count"`
}

func (projection PublicProjection) post(likedByMe bool) Post {
	return Post{
		EditedAt:        projection.EditedAt,
		ContentRevision: projection.ContentRevision,
		ID:              projection.ID,
		Title:           projection.Title,
		Content:         projection.Content,
		CreatedAt:       projection.CreatedAt,
		UpdatedAt:       projection.UpdatedAt,
		Author:          Author{ID: projection.Author.ID, Username: projection.Author.Username, DisplayName: projection.Author.DisplayName},
		CommentCount:    projection.CommentCount,
		LikeCount:       projection.LikeCount,
		LikedByMe:       likedByMe,
	}
}
