package notification

import (
	"errors"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
)

var (
	ErrInvalidArgument = errors.New("invalid notification argument")
	ErrNotFound        = errors.New("notification not found")
)

// Notification is the durable internal side effect produced from one business event.
type Notification struct {
	ID            uint64
	SourceEventID string
	Type          bus.EventType
	RecipientID   uint64
	ActorID       uint64
	PostID        uint64
	CommentID     *uint64
	CreatedAt     time.Time
	ReadAt        *time.Time
}

// Actor is the public user summary embedded in notification responses.
type Actor struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
}

// Public is the notification representation exposed to its recipient. Internal
// delivery identifiers and broker state are deliberately absent.
type Public struct {
	ID        uint64        `json:"id"`
	Type      bus.EventType `json:"type"`
	CreatedAt time.Time     `json:"created_at"`
	ReadAt    *time.Time    `json:"read_at"`
	Actor     Actor         `json:"actor"`
	PostID    uint64        `json:"post_id"`
	CommentID *uint64       `json:"comment_id"`
}

// Cursor is the stable keyset boundary decoded from an opaque client token.
type Cursor struct {
	CreatedAt time.Time
	ID        uint64
}

// ListOptions controls one recipient-scoped keyset query.
type ListOptions struct {
	Limit  int
	Cursor *Cursor
}

// Page contains one response page and its optional continuation token.
type Page struct {
	Notifications []Public
	NextCursor    *string
}
