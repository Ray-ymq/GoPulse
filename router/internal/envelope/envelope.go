package envelope

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"
	"unicode/utf8"
)

var messageIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

var requiredFields = map[string]struct{}{
	"schema_version": {},
	"message_id":     {},
	"type":           {},
	"source":         {},
	"timestamp":      {},
	"payload":        {},
}

type Message struct {
	MessageID string
	Type      string
	Source    string
	Body      []byte
}

type UnsupportedError struct{}

func (UnsupportedError) Error() string { return "message type is unsupported" }

func Validate(body []byte) (Message, error) {
	if !utf8.Valid(body) {
		return Message{}, errors.New("body is not valid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	token, err := dec.Token()
	if err != nil {
		return Message{}, errors.New("body is not valid JSON")
	}
	delim, ok := token.(json.Delim)
	if !ok || delim != '{' {
		return Message{}, errors.New("body must be a JSON object")
	}
	values := make(map[string]json.RawMessage, len(requiredFields))
	for dec.More() {
		keyToken, err := dec.Token()
		if err != nil {
			return Message{}, errors.New("body is not valid JSON")
		}
		key, ok := keyToken.(string)
		if !ok {
			return Message{}, errors.New("object key is invalid")
		}
		if _, allowed := requiredFields[key]; !allowed {
			return Message{}, fmt.Errorf("unknown top-level field %q", key)
		}
		if _, duplicate := values[key]; duplicate {
			return Message{}, fmt.Errorf("duplicate top-level field %q", key)
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return Message{}, errors.New("top-level field is invalid")
		}
		values[key] = raw
	}
	if token, err = dec.Token(); err != nil || token != json.Delim('}') {
		return Message{}, errors.New("body is not a complete JSON object")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Message{}, errors.New("body contains trailing data")
	}
	if len(values) != len(requiredFields) {
		return Message{}, errors.New("body is missing a required top-level field")
	}

	schemaToken := bytes.TrimSpace(values["schema_version"])
	if len(schemaToken) == 0 || (schemaToken[0] != '-' && (schemaToken[0] < '0' || schemaToken[0] > '9')) {
		return Message{}, errors.New("schema_version must be an integer")
	}
	var schema json.Number
	if err := json.Unmarshal(schemaToken, &schema); err != nil {
		return Message{}, errors.New("schema_version must be an integer")
	}
	schemaValue, err := schema.Int64()
	if err != nil {
		return Message{}, errors.New("schema_version must be an integer")
	}
	var messageID, messageType, source, timestamp string
	if json.Unmarshal(values["message_id"], &messageID) != nil || !messageIDPattern.MatchString(messageID) {
		return Message{}, errors.New("message_id is invalid")
	}
	if json.Unmarshal(values["type"], &messageType) != nil || messageType == "" {
		return Message{}, errors.New("type is invalid")
	}
	if json.Unmarshal(values["source"], &source) != nil || source == "" {
		return Message{}, errors.New("source is invalid")
	}
	if json.Unmarshal(values["timestamp"], &timestamp) != nil {
		return Message{}, errors.New("timestamp is invalid")
	}
	parsed, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil || parsed.Location() != time.UTC {
		return Message{}, errors.New("timestamp must be UTC RFC3339Nano")
	}
	payload := bytes.TrimSpace(values["payload"])
	if bytes.Equal(payload, []byte("null")) || len(payload) < 2 || payload[0] != '{' || payload[len(payload)-1] != '}' {
		return Message{}, errors.New("payload must be a non-null JSON object")
	}
	if schemaValue != 1 || !supported(messageType, source) {
		return Message{}, UnsupportedError{}
	}
	return Message{MessageID: messageID, Type: messageType, Source: source, Body: body}, nil
}

func supported(messageType, source string) bool {
	return (messageType == "metrics" && source == "redis") || (messageType == "logs" && source == "backend")
}
