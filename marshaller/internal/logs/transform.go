package logs

import (
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/Ray-ymq/GoPulse/marshaller/internal/envelope"
	"github.com/Ray-ymq/GoPulse/marshaller/internal/retention"
)

type Transformer struct {
	MaxBytes  int
	Retention *retention.Policy
	Now       func() time.Time
}

type WriteRequest struct {
	MessageID string          `json:"message_id"`
	IndexDate string          `json:"index_date"`
	Document  json.RawMessage `json:"document"`
	Items     []WriteItem     `json:"items,omitempty"`
}

type WriteItem struct {
	MessageID string          `json:"message_id"`
	IndexDate string          `json:"index_date"`
	Document  json.RawMessage `json:"document"`
}

func (t Transformer) Transform(message envelope.Envelope) ([]byte, error) {
	if message.Type != "logs" {
		return nil, &envelope.PermanentError{Code: "unsupported_envelope"}
	}
	if t.MaxBytes > 0 && len(message.RawPayload) > t.MaxBytes {
		return nil, &envelope.PermanentError{Code: "record_too_large"}
	}
	validated, err := Validate(message.RawPayload, message.Timestamp, 0)
	if err != nil {
		return nil, &envelope.PermanentError{Code: "invalid_log_payload"}
	}
	if validated.Source != message.Source {
		return nil, &envelope.PermanentError{Code: "source_mismatch"}
	}
	if len(validated.Entries) > 0 {
		return t.transformBatch(message, validated.Entries)
	}
	payloadTime, err := time.Parse(time.RFC3339Nano, validated.Timestamp)
	if err != nil || !payloadTime.Equal(message.Timestamp) {
		return nil, &envelope.PermanentError{Code: "timestamp_mismatch"}
	}
	if t.Retention != nil {
		now := time.Now()
		if t.Now != nil {
			now = t.Now()
		}
		if t.Retention.ExpiredTimestamp(message.Timestamp, now) {
			retention.ObserveLate(t.Retention.Stream, "expired")
			return nil, &envelope.PermanentError{Code: retention.PermanentCode(t.Retention.Stream)}
		}
		retention.ObserveLate(t.Retention.Stream, "accepted")
	}
	var fields map[string]any
	decoderBody := validated.Payload
	if err := json.Unmarshal(decoderBody, &fields); err != nil {
		return nil, &envelope.PermanentError{Code: "invalid_log_payload"}
	}
	delete(fields, "timestamp")
	fields["@timestamp"] = validated.Timestamp
	document, err := json.Marshal(fields)
	if err != nil {
		return nil, errors.New("log document serialization failed")
	}
	return json.Marshal(WriteRequest{MessageID: message.MessageID, IndexDate: message.Timestamp.UTC().Format("2006.01.02"), Document: document})
}

func (t Transformer) transformBatch(message envelope.Envelope, entries []Entry) ([]byte, error) {
	items := make([]WriteItem, 0, len(entries))
	for i, entry := range entries {
		at, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err != nil || at.Location() != time.UTC {
			return nil, &envelope.PermanentError{Code: "invalid_log_payload"}
		}
		if t.Retention != nil {
			now := time.Now()
			if t.Now != nil {
				now = t.Now()
			}
			if t.Retention.ExpiredTimestamp(at, now) {
				retention.ObserveLate(t.Retention.Stream, "expired")
				return nil, &envelope.PermanentError{Code: retention.PermanentCode(t.Retention.Stream)}
			}
		}
		document, err := json.Marshal(documentFields(entry))
		if err != nil {
			return nil, &envelope.PermanentError{Code: "transform_failed"}
		}
		id := message.MessageID
		if len(entries) > 1 {
			id += "-" + strconv.Itoa(i)
		}
		items = append(items, WriteItem{MessageID: id, IndexDate: at.UTC().Format("2006.01.02"), Document: document})
	}
	if len(items) == 1 {
		return json.Marshal(WriteRequest{MessageID: items[0].MessageID, IndexDate: items[0].IndexDate, Document: items[0].Document})
	}
	return json.Marshal(WriteRequest{MessageID: message.MessageID, Items: items})
}

func documentFields(entry Entry) map[string]any {
	fields := map[string]any{
		"@timestamp":         entry.Timestamp,
		"log_schema_version": 1,
		"level":              entry.Level,
		"service":            entry.Service,
		"module":             entry.Module,
		"message":            entry.Message,
	}
	for key, value := range entry.Fields {
		fields[key] = value
	}
	return fields
}
