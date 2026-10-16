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
	postDetailVersion        = 1
	postDetailKeyPrefix      = "gopulse:post:detail:v1:"
	postDetailEpochKeyPrefix = "gopulse:post:detail:v1:epoch:"
	postBodyVersion          = 2
	postBodyKeyPrefix        = "gopulse:post:body:v2:epoch:"
)

var ErrInvalidPostDetail = errors.New("invalid cached post detail")

type Client interface {
	Get(context.Context, string) (string, error)
	Set(context.Context, string, string, time.Duration) error
	Delete(context.Context, string) error
}

type PostDetailRepository struct {
	client           Client
	ttl              time.Duration
	operationTimeout time.Duration
	epochProvider    interface {
		CurrentEpoch(context.Context) (string, error)
	}
}

type postDetailEnvelope struct {
	Version int                   `json:"version"`
	Post    post.PublicProjection `json:"post"`
}

type postBodyEnvelope struct {
	Version int       `json:"version"`
	Body    post.Body `json:"body"`
}

func NewPostDetailRepository(client Client, ttl, operationTimeout time.Duration) *PostDetailRepository {
	return &PostDetailRepository{
		client:           client,
		ttl:              ttl,
		operationTimeout: operationTimeout,
	}
}

func (repository *PostDetailRepository) WithEpochProvider(provider interface {
	CurrentEpoch(context.Context) (string, error)
}) *PostDetailRepository {
	if repository != nil {
		repository.epochProvider = provider
	}
	return repository
}

func PostDetailKey(postID uint64) string {
	return postDetailKeyPrefix + strconv.FormatUint(postID, 10)
}

func PostDetailKeyForEpoch(epoch string, postID uint64) string {
	return postDetailEpochKeyPrefix + epoch + ":" + strconv.FormatUint(postID, 10)
}

// PostBodyKey formats the complete v2 key without resolving any part of it.
func PostBodyKey(key post.BodyKey) string {
	return postBodyKeyPrefix + key.Epoch + ":post:" + strconv.FormatUint(key.PostID, 10) + ":rev:" + strconv.FormatUint(key.ContentRevision, 10)
}

func (repository *PostDetailRepository) key(ctx context.Context, postID uint64) (string, error) {
	if repository.epochProvider == nil {
		return PostDetailKey(postID), nil
	}
	epoch, err := repository.epochProvider.CurrentEpoch(ctx)
	if err != nil || epoch == "" {
		return "", errors.New("read current business data epoch")
	}
	return PostDetailKeyForEpoch(epoch, postID), nil
}

func (repository *PostDetailRepository) Get(ctx context.Context, postID uint64) (post.PublicProjection, bool, error) {
	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()

	key, err := repository.key(operationContext, postID)
	if err != nil {
		return post.PublicProjection{}, false, err
	}
	value, err := repository.client.Get(operationContext, key)
	if errors.Is(err, platform.ErrRedisKeyNotFound) {
		return post.PublicProjection{}, false, nil
	}
	if err != nil {
		return post.PublicProjection{}, false, fmt.Errorf("read post detail cache: %w", err)
	}

	envelope, err := decodePostDetail([]byte(value))
	if err != nil || envelope.Version != postDetailVersion || !envelope.Post.ValidForKey(postID) {
		return post.PublicProjection{}, false, ErrInvalidPostDetail
	}
	return envelope.Post, true, nil
}

func (repository *PostDetailRepository) Set(ctx context.Context, projection post.PublicProjection) error {
	if !projection.ValidForKey(projection.ID) {
		return ErrInvalidPostDetail
	}
	payload, err := json.Marshal(postDetailEnvelope{Version: postDetailVersion, Post: projection})
	if err != nil {
		return fmt.Errorf("encode post detail cache: %w", err)
	}

	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	key, err := repository.key(operationContext, projection.ID)
	if err != nil {
		return err
	}
	if err := repository.client.Set(operationContext, key, string(payload), repository.ttl); err != nil {
		return fmt.Errorf("write post detail cache: %w", err)
	}
	return nil
}

func (repository *PostDetailRepository) Invalidate(ctx context.Context, postID uint64) error {
	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	key, err := repository.key(operationContext, postID)
	if err != nil {
		return err
	}
	if err := repository.client.Delete(operationContext, key); err != nil {
		return fmt.Errorf("invalidate post detail cache: %w", err)
	}
	return nil
}

// GetBody reads only the versioned v2 body. It deliberately does not call the
// legacy epoch provider, so an old in-flight request can never be rebound to a
// new epoch while it is being served.
func (repository *PostDetailRepository) GetBody(ctx context.Context, key post.BodyKey) (post.Body, bool, error) {
	observe := func(result string) {
		if metrics := componentmetrics.BackendActive(); metrics != nil {
			metrics.ObserveCacheRequest(result)
		}
	}
	if repository == nil || !validBodyKey(key) {
		observe("invalid")
		return post.Body{}, false, ErrInvalidPostDetail
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
		return post.Body{}, false, ErrInvalidPostDetail
	}
	observe("hit")
	return envelope.Body, true, nil
}

// SetBody writes a v2 body under the exact key supplied by the caller. A
// bounded jitter prevents a hot set of revisions expiring at one instant.
func (repository *PostDetailRepository) SetBody(ctx context.Context, key post.BodyKey, body post.Body) error {
	if repository == nil || !validBodyKey(key) || !body.ValidForKey(key) {
		return ErrInvalidPostDetail
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

func decodePostDetail(payload []byte) (postDetailEnvelope, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var envelope postDetailEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return postDetailEnvelope{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return postDetailEnvelope{}, errors.New("cached post detail contains trailing data")
	}
	return envelope, nil
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
