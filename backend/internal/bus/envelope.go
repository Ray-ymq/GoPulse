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

	"go.opentelemetry.io/otel/trace"
)

const (
	SchemaVersionV1 = 1
	SchemaVersionV2 = 2
	// SchemaVersion remains the production/default encoder version. Phase
	// 24-01 keeps normal producers on the legacy v1 shape.
	SchemaVersion = SchemaVersionV1

	MaxMessageBytes = 16 * 1024
	JSONContentType = "application/json"

	CommentCreatedRoutingKey = "comment.created.v1"
	UserFollowedRoutingKey   = "user.followed.v1"
	PostLikedRoutingKey      = "post.liked.v1"
	PostDeletedRoutingKey    = "post.deleted.v1"
	PostUpdatedRoutingKey    = "post.updated.v1"
	PostCreatedRoutingKey    = "post.created.v1"
)

type EventType string

const (
	CommentCreated EventType = "comment.created"
	UserFollowed   EventType = "user.followed"
	PostLiked      EventType = "post.liked"
	PostDeleted    EventType = "post.deleted"
	PostUpdated    EventType = "post.updated"
	PostCreated    EventType = "post.created"
)

type Envelope struct {
	ContentRevision uint64    `json:"content_revision,omitempty"`
	DataEpoch       string    `json:"data_epoch,omitempty"`
	CommandID       string    `json:"command_id,omitempty"`
	SchemaVersion   int       `json:"schema_version"`
	EventID         string    `json:"event_id"`
	EventType       EventType `json:"event_type"`
	OccurredAt      time.Time `json:"occurred_at"`
	ActorID         uint64    `json:"actor_id"`
	RecipientID     uint64    `json:"recipient_id,omitempty"`
	PostID          uint64    `json:"post_id,omitempty"`
	CommentID       *uint64   `json:"comment_id,omitempty"`
	TraceParent     string    `json:"traceparent,omitempty"`
	TraceState      string    `json:"tracestate,omitempty"`
	traceContextBad bool      `json:"-"`
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

func NewPostCreated(occurredAt time.Time, actorID, postID uint64) (Envelope, error) {
	return newEnvelope(PostCreated, occurredAt, actorID, 0, postID, nil)
}

// NewV2PostCreated is a controlled-activation constructor. Normal Phase
// 24-01 producers continue to use NewPostCreated and emit v1.
func NewV2PostCreated(occurredAt time.Time, epoch, commandID string, actorID, postID, revision uint64) (Envelope, error) {
	return newVersionedEnvelope(SchemaVersionV2, PostCreated, occurredAt, actorID, 0, postID, nil, epoch, commandID, revision)
}

func NewV2CommentCreated(occurredAt time.Time, epoch string, actorID, recipientID, postID, commentID uint64) (Envelope, error) {
	return newVersionedEnvelope(SchemaVersionV2, CommentCreated, occurredAt, actorID, recipientID, postID, &commentID, epoch, "", 0)
}

func NewV2UserFollowed(occurredAt time.Time, epoch string, follower, followed uint64) (Envelope, error) {
	return newVersionedEnvelope(SchemaVersionV2, UserFollowed, occurredAt, follower, followed, 0, nil, epoch, "", 0)
}

func NewV2PostLiked(occurredAt time.Time, epoch string, actorID, recipientID, postID uint64) (Envelope, error) {
	return newVersionedEnvelope(SchemaVersionV2, PostLiked, occurredAt, actorID, recipientID, postID, nil, epoch, "", 0)
}

func NewV2PostUpdated(occurredAt time.Time, epoch string, actorID, postID, revision uint64) (Envelope, error) {
	return newVersionedEnvelope(SchemaVersionV2, PostUpdated, occurredAt, actorID, 0, postID, nil, epoch, "", revision)
}

func NewV2PostDeleted(occurredAt time.Time, epoch string, actorID, postID uint64) (Envelope, error) {
	return newVersionedEnvelope(SchemaVersionV2, PostDeleted, occurredAt, actorID, 0, postID, nil, epoch, "", 0)
}

// NewUUID returns a canonical UUID for a durable business identity. The
// event constructors use the same generator, but commands need their own
// identity before the post-created envelope is encoded.
func NewUUID() (string, error) { return newUUID() }

// WithDataEpoch upgrades an existing event while preserving its event ID.
// Producers call this inside their fact transaction so every v2 event carries
// the same epoch as the fact it accompanies. post.created additionally needs a
// command ID and therefore must be constructed with NewV2PostCreated.
func (envelope Envelope) WithDataEpoch(epoch string) (Envelope, error) {
	if !validUUID(epoch) {
		return Envelope{}, errors.New("business event data epoch must be a canonical UUID")
	}
	envelope.SchemaVersion = SchemaVersionV2
	envelope.DataEpoch = epoch
	if err := envelope.Validate(); err != nil {
		return Envelope{}, err
	}
	return envelope, nil
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

func newVersionedEnvelope(version int, eventType EventType, occurredAt time.Time, actorID, recipientID, postID uint64, commentID *uint64, epoch, commandID string, revision uint64) (Envelope, error) {
	eventID, err := newUUID()
	if err != nil {
		return Envelope{}, err
	}
	event := Envelope{
		SchemaVersion: version, EventID: eventID, EventType: eventType, OccurredAt: occurredAt.UTC(),
		ActorID: actorID, RecipientID: recipientID, PostID: postID, CommentID: commentID,
		DataEpoch: epoch, CommandID: commandID, ContentRevision: revision,
	}
	if err := event.Validate(); err != nil {
		return Envelope{}, err
	}
	return event, nil
}

func (envelope Envelope) Validate() error {
	if envelope.SchemaVersion != SchemaVersionV1 && envelope.SchemaVersion != SchemaVersionV2 {
		return fmt.Errorf("business event schema version must be %d or %d", SchemaVersionV1, SchemaVersionV2)
	}
	if envelope.SchemaVersion == SchemaVersionV1 {
		if envelope.DataEpoch != "" || envelope.CommandID != "" {
			return errors.New("v1 business event must not contain generation identity")
		}
	} else if !validUUID(envelope.DataEpoch) {
		return errors.New("v2 business event data epoch must be a canonical UUID")
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
	if envelope.ActorID == 0 || (envelope.EventType != UserFollowed && envelope.PostID == 0) {
		return errors.New("business event actor and post IDs must be positive")
	}

	switch envelope.EventType {
	case UserFollowed:
		if envelope.RecipientID == 0 || envelope.ActorID == envelope.RecipientID || envelope.PostID != 0 || envelope.CommentID != nil {
			return errors.New("invalid user.followed shape")
		}
	case CommentCreated:
		if envelope.RecipientID == 0 {
			return errors.New("comment.created event requires a positive recipient ID")
		}
		if envelope.CommentID == nil || *envelope.CommentID == 0 {
			return errors.New("comment.created event requires a positive comment ID")
		}
	case PostLiked:
		if envelope.RecipientID == 0 {
			return errors.New("post.liked event requires a positive recipient ID")
		}
		if envelope.CommentID != nil {
			return errors.New("post.liked event must not include a comment ID")
		}
	case PostCreated, PostUpdated, PostDeleted:
		if envelope.EventType == PostUpdated && envelope.ContentRevision < 2 {
			return errors.New("post.updated requires revision")
		}
		if envelope.SchemaVersion == SchemaVersionV2 && envelope.EventType == PostCreated {
			if !validUUID(envelope.CommandID) {
				return errors.New("v2 post.created requires a command ID")
			}
			if envelope.ContentRevision == 0 {
				return errors.New("v2 post.created requires a positive content revision")
			}
		} else if envelope.CommandID != "" {
			return errors.New("command ID is only valid for v2 post.created")
		}
		if envelope.RecipientID != 0 || envelope.CommentID != nil {
			return errors.New("post event must not include recipient or comment IDs")
		}
	default:
		return errors.New("business event type is unsupported")
	}
	return nil
}

// CompatibilityClass is explicit because v1 carries no epoch and can never
// satisfy a v2 application-completion contract.
type CompatibilityClass string

const (
	LegacyUnknown CompatibilityClass = "legacy_unknown"
	EpochV2       CompatibilityClass = "v2"
)

func (envelope Envelope) CompatibilityClass() CompatibilityClass {
	if envelope.SchemaVersion == SchemaVersionV2 {
		return EpochV2
	}
	return LegacyUnknown
}

func (envelope Envelope) RoutingKey() (string, error) {
	if err := envelope.Validate(); err != nil {
		return "", err
	}
	switch envelope.EventType {
	case UserFollowed:
		return UserFollowedRoutingKey, nil
	case CommentCreated:
		return CommentCreatedRoutingKey, nil
	case PostLiked:
		return PostLikedRoutingKey, nil
	case PostDeleted:
		return PostDeletedRoutingKey, nil
	case PostUpdated:
		return PostUpdatedRoutingKey, nil
	case PostCreated:
		return PostCreatedRoutingKey, nil
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
	envelope.normalizeTraceContext()
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
	envelope.normalizeTraceContext()
	return envelope, nil
}

// SetTraceContext stores only the two W3C fields that are allowed to cross a
// business-message boundary. Invalid optional values are omitted by Encode so
// an otherwise valid business event remains processable.
func (envelope *Envelope) SetTraceContext(traceParent, traceState string) {
	if envelope == nil {
		return
	}
	envelope.TraceParent = traceParent
	envelope.TraceState = traceState
	envelope.normalizeTraceContext()
}

func (envelope Envelope) HasInvalidTraceContext() bool { return envelope.traceContextBad }

func (envelope Envelope) TraceContext() (traceParent, traceState string, valid bool) {
	if envelope.traceContextBad || !validTraceParent(envelope.TraceParent) {
		return "", "", false
	}
	if envelope.TraceState != "" && !validTraceState(envelope.TraceState) {
		return "", "", false
	}
	return envelope.TraceParent, envelope.TraceState, true
}

func (envelope *Envelope) normalizeTraceContext() {
	if envelope == nil {
		return
	}
	if envelope.TraceParent == "" && envelope.TraceState == "" {
		return
	}
	if !validTraceParent(envelope.TraceParent) || (envelope.TraceState != "" && !validTraceState(envelope.TraceState)) {
		envelope.TraceParent = ""
		envelope.TraceState = ""
		envelope.traceContextBad = true
	}
}

func validTraceParent(value string) bool {
	if len(value) != 55 || value[2] != '-' || value[35] != '-' || value[52] != '-' {
		return false
	}
	if !validHex(value[:2]) || value[:2] == "ff" || !validHex(value[3:35]) || allZero(value[3:35]) || !validHex(value[36:52]) || allZero(value[36:52]) || !validHex(value[53:55]) {
		return false
	}
	return true
}

func validTraceState(value string) bool {
	if value == "" || len(value) > 512 {
		return false
	}
	_, err := trace.ParseTraceState(value)
	return err == nil
}

func validHex(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func allZero(value string) bool {
	for _, character := range value {
		if character != '0' {
			return false
		}
	}
	return true
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
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func NewUserFollowed(at time.Time, follower, followed uint64) (Envelope, error) {
	return newEnvelope(UserFollowed, at, follower, followed, 0, nil)
}

func NewPostUpdated(at time.Time, actor, postID, revision uint64) (Envelope, error) {
	e, err := NewPostCreated(at, actor, postID)
	if err != nil {
		return Envelope{}, err
	}
	e.EventType = PostUpdated
	e.ContentRevision = revision
	return e, e.Validate()
}

func NewPostDeleted(at time.Time, actor, postID uint64) (Envelope, error) {
	return newEnvelope(PostDeleted, at, actor, 0, postID, nil)
}
