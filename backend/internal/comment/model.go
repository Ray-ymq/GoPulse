package comment

import "time"

// CreateInput is the only client-controlled input accepted when publishing a comment.
type CreateInput struct {
	Content string `json:"content"`
}

// Author is the public author summary embedded in comment responses.
type Author struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
}

// Comment is the complete comment read model returned by create and list APIs.
type Comment struct {
	ID        uint64    `json:"id"`
	PostID    uint64    `json:"post_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	Author    Author    `json:"author"`
}

// Cursor is the stable keyset boundary decoded from an opaque client token.
type Cursor struct {
	ID uint64
}

// ListOptions controls one keyset-paginated comment list query.
type ListOptions struct {
	Limit  int
	Cursor *Cursor
}

// Page contains one response page and its optional continuation token.
type Page struct {
	Comments   []Comment
	NextCursor *string
}
