package logs

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/Ray-ymq/GoPulse/marshaller/internal/envelope"
)

type Transformer struct{ MaxBytes int }

type WriteRequest struct {
	MessageID string          `json:"message_id"`
	IndexDate string          `json:"index_date"`
	Document  json.RawMessage `json:"document"`
}

func (t Transformer) Transform(message envelope.Envelope) ([]byte, error) {
	if message.Type != "logs" || message.Source != "backend" {
		return nil, &envelope.PermanentError{Code: "unsupported_envelope"}
	}
	if t.MaxBytes > 0 && len(message.RawPayload) > t.MaxBytes {
		return nil, &envelope.PermanentError{Code: "record_too_large"}
	}
	validated, err := Validate(message.RawPayload, message.Timestamp, 0)
	if err != nil {
		return nil, &envelope.PermanentError{Code: "invalid_log_payload"}
	}
	payloadTime, err := time.Parse(time.RFC3339Nano, validated.Timestamp)
	if err != nil || !payloadTime.Equal(message.Timestamp) {
		return nil, &envelope.PermanentError{Code: "timestamp_mismatch"}
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
