package recipe

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

type outboxEvent struct {
	Type        string
	EventID     string
	OccurredAt  time.Time
	ActorID     uint64
	RecipientID uint64
	PostID      uint64
	CommentID   uint64
}

type eventPayload struct {
	ContentRevision uint64    `json:"content_revision,omitempty"`
	SchemaVersion   int       `json:"schema_version"`
	EventID         string    `json:"event_id"`
	EventType       string    `json:"event_type"`
	OccurredAt      time.Time `json:"occurred_at"`
	ActorID         uint64    `json:"actor_id"`
	RecipientID     uint64    `json:"recipient_id,omitempty"`
	PostID          uint64    `json:"post_id,omitempty"`
	CommentID       *uint64   `json:"comment_id,omitempty"`
}

func eventID(seed uint64, eventType string, sequence uint64) string {
	var material [16]byte
	binary.BigEndian.PutUint64(material[0:8], seed)
	binary.BigEndian.PutUint64(material[8:16], sequence)
	digest := sha256.Sum256(append([]byte(eventType+"\x00"), material[:]...))
	value := digest[:16]
	value[6] = (value[6] & 0x0f) | 0x50
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], value[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], value[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], value[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], value[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], value[10:16])
	return string(encoded)
}

func newOutboxEvent(seed uint64, eventType string, sequence uint64, occurredAt time.Time, actorID, recipientID, postID, commentID uint64) outboxEvent {
	return outboxEvent{
		Type: eventType, EventID: eventID(seed, eventType, sequence), OccurredAt: occurredAt.UTC(),
		ActorID: actorID, RecipientID: recipientID, PostID: postID, CommentID: commentID,
	}
}

func forEachOutboxEvent(seed uint64, visit func(outboxEvent) error) error {
	sequence := uint64(0)
	post := func(id uint64) error {
		sequence++
		event := newOutboxEvent(seed, "post.created", sequence, rowTime(1000000+id), postAuthor(id), 0, id, 0)
		return visit(event)
	}
	comment := func(id uint64) error {
		sequence++
		postID := commentPost(id)
		event := newOutboxEvent(seed, "comment.created", sequence, rowTime(2000000+id), commentAuthor(id), postAuthor(postID), postID, id)
		return visit(event)
	}
	like := func(id uint64) error {
		sequence++
		postID := likePost(id)
		event := newOutboxEvent(seed, "post.liked", sequence, rowTime(3000000+id), likeUser(id), postAuthor(postID), postID, 0)
		return visit(event)
	}
	follow := func(id uint64) error {
		sequence++
		event := newOutboxEvent(seed, "user.followed", sequence, rowTime(4000000+id), followFollower(id), followTarget(id), 0, 0)
		return visit(event)
	}
	for id := uint64(1); id <= 50000; id++ {
		if err := post(id); err != nil {
			return err
		}
	}
	for id := uint64(1); id <= 100000; id++ {
		if err := comment(id); err != nil {
			return err
		}
	}
	for id := uint64(1); id <= 200000; id++ {
		if err := like(id); err != nil {
			return err
		}
	}
	for id := uint64(1); id <= 200000; id++ {
		if err := follow(id); err != nil {
			return err
		}
	}
	return nil
}

func (event outboxEvent) payload() ([]byte, error) {
	value := eventPayload{
		SchemaVersion: 1, EventID: event.EventID, EventType: event.Type,
		OccurredAt: event.OccurredAt.UTC(), ActorID: event.ActorID,
		RecipientID: event.RecipientID, PostID: event.PostID,
	}
	if event.CommentID != 0 {
		commentID := event.CommentID
		value.CommentID = &commentID
	}
	return json.Marshal(value)
}

func (event outboxEvent) validate() error {
	if event.EventID == "" || event.Type == "" || event.OccurredAt.IsZero() || event.ActorID == 0 {
		return fmt.Errorf("invalid deterministic outbox event")
	}
	if event.Type != "user.followed" && event.PostID == 0 {
		return fmt.Errorf("event requires post ID")
	}
	return nil
}

// notificationFact is the deterministic notification projection of a single
// business event. RecipientID, PostID, and CommentID mirror the rules enforced
// by the notification repository: notifications are written from the event
// envelope, comments carry both resources, likes carry only the post, and
// follows carry neither.
type notificationFact struct {
	SourceEventID string
	Type          string
	RecipientID   uint64
	ActorID       uint64
	PostID        uint64
	CommentID     uint64
	CreatedAt     time.Time
}

// notification derives the notification projected from one outbox event. The
// second result is false for event types that never notify anyone and for
// self-directed facts, which the product deliberately drops.
func (event outboxEvent) notification() (notificationFact, bool) {
	if event.RecipientID == 0 || event.ActorID == event.RecipientID {
		return notificationFact{}, false
	}
	fact := notificationFact{
		SourceEventID: event.EventID,
		Type:          event.Type,
		RecipientID:   event.RecipientID,
		ActorID:       event.ActorID,
		CreatedAt:     event.OccurredAt.UTC(),
	}
	switch event.Type {
	case "comment.created":
		fact.PostID = event.PostID
		fact.CommentID = event.CommentID
	case "post.liked":
		fact.PostID = event.PostID
	case "user.followed":
	default:
		return notificationFact{}, false
	}
	return fact, true
}

// forEachNotification visits every deterministic notification in the same
// order as the outbox facts they are projected from.
func forEachNotification(seed uint64, visit func(notificationFact) error) error {
	return forEachOutboxEvent(seed, func(event outboxEvent) error {
		fact, ok := event.notification()
		if !ok {
			return nil
		}
		return visit(fact)
	})
}
