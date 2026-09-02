package notification

import (
	"errors"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
)

var ErrInvalidArgument = errors.New("invalid notification argument")

// Notification is the durable side effect produced from one business event.
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
