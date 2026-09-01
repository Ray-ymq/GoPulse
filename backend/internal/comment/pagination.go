package comment

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strconv"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
)

const (
	DefaultLimit  = 20
	MaximumLimit  = 50
	cursorVersion = 1
)

type cursorPayload struct {
	Version int    `json:"v"`
	ID      uint64 `json:"id"`
}

// EncodeCursor serializes a comment keyset boundary as an unpadded URL-safe Base64 token.
func EncodeCursor(cursor Cursor) (string, error) {
	if cursor.ID == 0 {
		return "", errors.New("cursor requires a positive ID")
	}
	payload, err := json.Marshal(cursorPayload{Version: cursorVersion, ID: cursor.ID})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// DecodeCursor strictly validates the token version, JSON shape, and positive identifier.
func DecodeCursor(token string) (Cursor, error) {
	if token == "" {
		return Cursor{}, errors.New("cursor is empty")
	}
	if len(token) > 256 {
		return Cursor{}, errors.New("cursor is too large")
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != token {
		return Cursor{}, errors.New("cursor is not valid canonical URL-safe Base64")
	}

	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var payload cursorPayload
	if err := decoder.Decode(&payload); err != nil {
		return Cursor{}, errors.New("cursor payload is invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Cursor{}, errors.New("cursor payload must contain one object")
	}
	if payload.Version != cursorVersion || payload.ID == 0 {
		return Cursor{}, errors.New("cursor payload fields are invalid")
	}
	return Cursor{ID: payload.ID}, nil
}

// ParseListOptions validates the complete comment-list query.
func ParseListOptions(values url.Values) (ListOptions, error) {
	options := ListOptions{Limit: DefaultLimit}
	if limits, ok := values["limit"]; ok {
		if len(limits) != 1 || !decimalDigits(limits[0]) {
			return ListOptions{}, validationError("limit must be an integer between 1 and 50")
		}
		limit, err := strconv.ParseUint(limits[0], 10, 8)
		if err != nil || limit < 1 || limit > MaximumLimit {
			return ListOptions{}, validationError("limit must be an integer between 1 and 50")
		}
		options.Limit = int(limit)
	}

	if cursors, ok := values["cursor"]; ok {
		if len(cursors) != 1 {
			return ListOptions{}, validationError("cursor is invalid")
		}
		cursor, err := DecodeCursor(cursors[0])
		if err != nil {
			return ListOptions{}, validationError("cursor is invalid")
		}
		options.Cursor = &cursor
	}
	return options, nil
}

func decimalDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func validationError(message string) error {
	return apperror.New(apperror.CodeValidationFailed, message)
}
