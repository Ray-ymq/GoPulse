package rediscache

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
)

const (
	postDetailVersion        = 1
	postDetailKeyPrefix      = "gopulse:post:detail:v1:"
	postDetailEpochKeyPrefix = "gopulse:post:detail:v1:epoch:"
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
