package rediscache

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
)

const (
	postBodyVersion   = 2
	postBodyKeyPrefix = "gopulse:post:body:v2:epoch:"
)

var ErrInvalidPostBody = errors.New("invalid cached post body")

type Client interface {
	Get(context.Context, string) (string, error)
	Set(context.Context, string, string, time.Duration) error
}

type PostBodyRepository struct {
	client           Client
	ttl              time.Duration
	operationTimeout time.Duration
}

type postBodyEnvelope struct {
	Version int       `json:"version"`
	Body    post.Body `json:"body"`
}

func NewPostBodyRepository(client Client, ttl, operationTimeout time.Duration) *PostBodyRepository {
	return &PostBodyRepository{
		client:           client,
		ttl:              ttl,
		operationTimeout: operationTimeout,
	}
}

// PostBodyKey formats the complete v2 key without resolving any part of it.
func PostBodyKey(key post.BodyKey) string {
	return postBodyKeyPrefix + key.Epoch + ":post:" + strconv.FormatUint(key.PostID, 10) + ":rev:" + strconv.FormatUint(key.ContentRevision, 10)
}

// GetBody reads the exact epoch and revision fixed by the authoritative read context.
func (repository *PostBodyRepository) GetBody(ctx context.Context, key post.BodyKey) (post.Body, bool, error) {
	observe := func(result string) {
		if metrics := componentmetrics.BackendActive(); metrics != nil {
			metrics.ObserveCacheRequest(result)
		}
	}
	if repository == nil || !validBodyKey(key) {
		observe("invalid")
		return post.Body{}, false, ErrInvalidPostBody
	}
	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	value, err := repository.client.Get(operationContext, PostBodyKey(key))
	if errors.Is(err, platform.ErrRedisKeyNotFound) {
		observe("miss")
		return post.Body{}, false, nil
	}
	if err != nil {
		observe("error")
		return post.Body{}, false, fmt.Errorf("read post body cache: %w", err)
	}
	envelope, err := decodePostBody([]byte(value))
	if err != nil || envelope.Version != postBodyVersion || !envelope.Body.ValidForKey(key) {
		observe("invalid")
		return post.Body{}, false, ErrInvalidPostBody
	}
	observe("hit")
	return envelope.Body, true, nil
}

// SetBody writes a v2 body under the exact key supplied by the caller. A
// bounded jitter prevents a hot set of revisions expiring at one instant.
func (repository *PostBodyRepository) SetBody(ctx context.Context, key post.BodyKey, body post.Body) error {
	if repository == nil || !validBodyKey(key) || !body.ValidForKey(key) {
		return ErrInvalidPostBody
	}
	payload, err := json.Marshal(postBodyEnvelope{Version: postBodyVersion, Body: body})
	if err != nil {
		return fmt.Errorf("encode post body cache: %w", err)
	}
	ttl := repository.ttl
	if ttl <= 0 {
		return errors.New("post body cache TTL is invalid")
	}
	// Keep the initial contract at +/-10%; the result is always positive.
	jitter := 0.10 * (rand.Float64()*2 - 1)
	ttl = time.Duration(float64(ttl) * (1 + jitter))
	if ttl <= 0 {
		ttl = time.Millisecond
	}
	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	if err := repository.client.Set(operationContext, PostBodyKey(key), string(payload), ttl); err != nil {
		return fmt.Errorf("write post body cache: %w", err)
	}
	return nil
}

func validBodyKey(key post.BodyKey) bool {
	return key.Valid() && len(key.Epoch) <= 64 && !strings.ContainsAny(key.Epoch, ":\r\n\t ")
}

func decodePostBody(payload []byte) (postBodyEnvelope, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var envelope postBodyEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return postBodyEnvelope{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return postBodyEnvelope{}, errors.New("cached post body contains trailing data")
	}
	return envelope, nil
}
