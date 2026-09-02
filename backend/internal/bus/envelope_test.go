package bus

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestBusinessEventsRoundTripWithStableMetadata(t *testing.T) {
	occurredAt := time.Date(2026, time.September, 2, 2, 3, 4, 567890123, time.UTC)
	comment, err := NewCommentCreated(occurredAt, 11, 22, 33, 44)
	if err != nil {
		t.Fatalf("NewCommentCreated() error = %v", err)
	}
	liked, err := NewPostLiked(occurredAt, 55, 66, 77)
	if err != nil {
		t.Fatalf("NewPostLiked() error = %v", err)
	}

	for _, test := range []struct {
		name       string
		envelope   Envelope
		routingKey string
	}{
		{name: "comment", envelope: comment, routingKey: CommentCreatedRoutingKey},
		{name: "like", envelope: liked, routingKey: PostLikedRoutingKey},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, err := Encode(test.envelope)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			decoded, err := Decode(body)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if decoded.EventID != test.envelope.EventID || decoded.EventType != test.envelope.EventType || !decoded.OccurredAt.Equal(occurredAt) {
				t.Fatalf("decoded envelope = %#v, want %#v", decoded, test.envelope)
			}
			routingKey, err := decoded.RoutingKey()
			if err != nil || routingKey != test.routingKey {
				t.Fatalf("RoutingKey() = %q, %v; want %q", routingKey, err, test.routingKey)
			}
			metadata, err := decoded.Metadata()
			if err != nil {
				t.Fatalf("Metadata() error = %v", err)
			}
			if metadata.MessageID != decoded.EventID || metadata.ContentType != JSONContentType || metadata.Type != string(decoded.EventType) || !metadata.Timestamp.Equal(occurredAt) {
				t.Fatalf("metadata = %#v", metadata)
			}
		})
	}
}

func TestDecodeRejectsInvalidBusinessEvents(t *testing.T) {
	valid := `{"schema_version":1,"event_id":"123e4567-e89b-12d3-a456-426614174000","event_type":"comment.created","occurred_at":"2026-09-02T02:03:04Z","actor_id":1,"recipient_id":2,"post_id":3,"comment_id":4}`
	tests := map[string]string{
		"empty":             "",
		"unknown field":     strings.Replace(valid, `"comment_id":4`, `"comment_id":4,"content":"secret"`, 1),
		"multiple values":   valid + `{}`,
		"unknown version":   strings.Replace(valid, `"schema_version":1`, `"schema_version":2`, 1),
		"unknown type":      strings.Replace(valid, `"comment.created"`, `"comment.deleted"`, 1),
		"non UTC":           strings.Replace(valid, `"2026-09-02T02:03:04Z"`, `"2026-09-02T10:03:04+08:00"`, 1),
		"zero actor":        strings.Replace(valid, `"actor_id":1`, `"actor_id":0`, 1),
		"missing comment":   strings.Replace(valid, `,"comment_id":4`, ``, 1),
		"like with comment": strings.Replace(valid, `"comment.created"`, `"post.liked"`, 1),
		"bad UUID":          strings.Replace(valid, `123e4567-e89b-12d3-a456-426614174000`, `not-a-uuid`, 1),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode([]byte(body)); err == nil {
				t.Fatal("Decode() error = nil")
			}
		})
	}
}

func TestDecodeRejectsOversizedBodyBeforeJSONParsing(t *testing.T) {
	body := bytes.Repeat([]byte{'x'}, MaxMessageBytes+1)
	if _, err := Decode(body); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("Decode() error = %v, want size limit", err)
	}
}

func TestConstructorsRejectInvalidIdentifiers(t *testing.T) {
	if _, err := NewCommentCreated(time.Now(), 0, 2, 3, 4); err == nil {
		t.Fatal("NewCommentCreated() error = nil")
	}
	if _, err := NewPostLiked(time.Now(), 1, 2, 0); err == nil {
		t.Fatal("NewPostLiked() error = nil")
	}
}

func TestPostCreatedOmitsNotificationFields(t *testing.T) {
	occurredAt := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)
	event, err := NewPostCreated(occurredAt, 11, 22)
	if err != nil {
		t.Fatal(err)
	}
	body, err := Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte("recipient_id")) || bytes.Contains(body, []byte("comment_id")) || bytes.Contains(body, []byte("title")) || bytes.Contains(body, []byte("content")) {
		t.Fatalf("post.created payload leaks projection or notification fields: %s", body)
	}
	decoded, err := Decode(body)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.EventType != PostCreated || decoded.ActorID != 11 || decoded.PostID != 22 || decoded.RecipientID != 0 || decoded.CommentID != nil {
		t.Fatalf("decoded post.created = %#v", decoded)
	}
	if key, err := decoded.RoutingKey(); err != nil || key != PostCreatedRoutingKey {
		t.Fatalf("RoutingKey() = %q, %v", key, err)
	}
}
