package post

import "time"

// CreateInput is the only client-controlled input accepted when publishing a post.
type CreateInput struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// Author is the public author summary embedded in post responses.
type Author struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
}

// Post is the complete post read model returned by create, list, and detail APIs.
type Post struct {
	ID           uint64    `json:"id"`
	Title        string    `json:"title"`
	Content      string    `json:"content"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Author       Author    `json:"author"`
	CommentCount uint64    `json:"comment_count"`
	LikeCount    uint64    `json:"like_count"`
	LikedByMe    bool      `json:"liked_by_me"`
}

// Cursor is the stable keyset boundary decoded from an opaque client token.
type Cursor struct {
	CreatedAt time.Time
	ID        uint64
}

// ListOptions controls one keyset-paginated list query.
type ListOptions struct {
	Limit  int
	Cursor *Cursor
}

// Page contains one response page and its optional continuation token.
type Page struct {
	Posts      []Post
	NextCursor *string
}
