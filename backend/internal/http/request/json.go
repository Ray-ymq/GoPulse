package request

import (
	"encoding/json"
	"errors"
	"io"
	stdhttp "net/http"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/gin-gonic/gin"
)

const DefaultJSONBodyLimit int64 = 64 << 10

// DecodeJSON strictly decodes one JSON object using the default business-body limit.
func DecodeJSON(c *gin.Context, destination any) error {
	return DecodeJSONWithLimit(c, destination, DefaultJSONBodyLimit)
}

// DecodeJSONWithLimit strictly decodes exactly one JSON value and rejects unknown fields.
func DecodeJSONWithLimit(c *gin.Context, destination any, limit int64) error {
	if destination == nil {
		return apperror.New(apperror.CodeValidationFailed, "request body destination is required")
	}
	if limit <= 0 {
		return apperror.New(apperror.CodeValidationFailed, "request body limit must be positive")
	}

	c.Request.Body = stdhttp.MaxBytesReader(c.Writer, c.Request.Body, limit)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(destination); err != nil {
		return decodeError(err, limit)
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return apperror.New(apperror.CodeValidationFailed, "request body must contain a single JSON value")
		}
		return decodeError(err, limit)
	}
	return nil
}

func decodeError(err error, limit int64) error {
	var maxBytesError *stdhttp.MaxBytesError
	if errors.As(err, &maxBytesError) {
		return apperror.New(apperror.CodeValidationFailed, "request body exceeds the allowed size")
	}
	if errors.Is(err, io.EOF) {
		return apperror.New(apperror.CodeValidationFailed, "request body is required")
	}
	return apperror.New(apperror.CodeValidationFailed, "request body must be valid JSON")
}
