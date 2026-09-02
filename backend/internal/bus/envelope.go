package bus

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	SchemaVersion = 1

	MaxMessageBytes = 16 * 1024
	JSONContentType = "application/json"

	CommentCreatedRoutingKey = "comment.created.v1"
	PostLikedRoutingKey      = "post.liked.v1"
)

type EventType string

const (
	CommentCreated EventType = "comment.created"
	PostLiked      EventType = "post.liked"
)

type Envelope struct {
	SchemaVersion int       `json:"schema_version"`
	EventID       string    `json:"event_id"`
	EventType     EventType `json:"event_type"`
	OccurredAt    time.Time `json:"occurred_at"`
	ActorID       uint64    `json:"actor_id"`
	RecipientID   uint64    `json:"recipient_id"`
	PostID        uint64    `json:"post_id"`
	CommentID     *uint64   `json:"comment_id,omitempty"`
}

type Metadata struct {
	MessageID   string
	ContentType string
	Type        string
	Timestamp   time.Time
}

func NewCommentCreated(occurredAt time.Time, actorID, recipientID, postID, commentID uint64) (Envelope, error) {
	return newEnvelope(CommentCreated, occurredAt, actorID, recipientID, postID, &commentID)
}

func NewPostLiked(occurredAt time.Time, actorID, recipientID, postID uint64) (Envelope, error) {
	return newEnvelope(PostLiked, occurredAt, actorID, recipientID, postID, nil)
}

func newEnvelope(eventType EventType, occurredAt time.Time, actorID, recipientID, postID uint64, commentID *uint64) (Envelope, error) {
	eventID, err := newUUID()
	if err != nil {
		return Envelope{}, errors.New("generate business event ID")
	}
	envelope := Envelope{
		SchemaVersion: SchemaVersion,
		EventID:       eventID,
		EventType:     eventType,
		OccurredAt:    occurredAt.UTC(),
		ActorID:       actorID,
		RecipientID:   recipientID,
		PostID:        postID,
		CommentID:     commentID,
	}
	if err := envelope.Validate(); err != nil {
		return Envelope{}, err
	}
	return envelope, nil
}

func (envelope Envelope) Validate() error {
	if envelope.SchemaVersion != SchemaVersion {
		return fmt.Errorf("business event schema version must be %d", SchemaVersion)
	}
	if !validUUID(envelope.EventID) {
		return errors.New("business event ID must be a canonical UUID")
	}
	if envelope.OccurredAt.IsZero() {
		return errors.New("business event occurrence time is required")
	}
	_, offset := envelope.OccurredAt.Zone()
	if offset != 0 {
		return errors.New("business event occurrence time must be UTC")
	}
	if envelope.ActorID == 0 || envelope.RecipientID == 0 || envelope.PostID == 0 {
		return errors.New("business event actor, recipient, and post IDs must be positive")
	}

	switch envelope.EventType {
	case CommentCreated:
		if envelope.CommentID == nil || *envelope.CommentID == 0 {
			return errors.New("comment.created event requires a positive comment ID")
		}
	case PostLiked:
		if envelope.CommentID != nil {
			return errors.New("post.liked event must not include a comment ID")
		}
	default:
		return errors.New("business event type is unsupported")
	}
	return nil
}

func (envelope Envelope) RoutingKey() (string, error) {
	if err := envelope.Validate(); err != nil {
		return "", err
	}
	switch envelope.EventType {
	case CommentCreated:
		return CommentCreatedRoutingKey, nil
	case PostLiked:
		return PostLikedRoutingKey, nil
	default:
		return "", errors.New("business event type is unsupported")
	}
}

func (envelope Envelope) Metadata() (Metadata, error) {
	if err := envelope.Validate(); err != nil {
		return Metadata{}, err
	}
	return Metadata{
		MessageID:   envelope.EventID,
		ContentType: JSONContentType,
		Type:        string(envelope.EventType),
		Timestamp:   envelope.OccurredAt.UTC(),
	}, nil
}

func Encode(envelope Envelope) ([]byte, error) {
	if err := envelope.Validate(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, errors.New("encode business event")
	}
	if len(body) > MaxMessageBytes {
		return nil, errors.New("business event exceeds message size limit")
	}
	return body, nil
}

func Decode(body []byte) (Envelope, error) {
	if len(body) == 0 {
		return Envelope{}, errors.New("business event body is empty")
	}
	if len(body) > MaxMessageBytes {
		return Envelope{}, errors.New("business event exceeds message size limit")
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var envelope Envelope
	if err := decoder.Decode(&envelope); err != nil {
		return Envelope{}, errors.New("decode business event")
	}
	if err := requireJSONEnd(decoder); err != nil {
		return Envelope{}, err
	}
	if err := envelope.Validate(); err != nil {
		return Envelope{}, err
	}
	return envelope, nil
}

func requireJSONEnd(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return errors.New("business event body must contain exactly one JSON value")
}

func newUUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], raw[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], raw[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], raw[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], raw[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], raw[10:16])
	return string(encoded), nil
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for index := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		character := value[index]
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return false
		}
	}
	return true
}
