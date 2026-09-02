package search

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/worker"
)

type DocumentStore interface {
	FindDocument(context.Context, uint64) (Document, error)
}

type MySQLDocumentStore struct{ database *sql.DB }

func NewMySQLDocumentStore(database *sql.DB) *MySQLDocumentStore {
	return &MySQLDocumentStore{database: database}
}

func (store *MySQLDocumentStore) FindDocument(ctx context.Context, postID uint64) (Document, error) {
	if store == nil || store.database == nil || postID == 0 {
		return Document{}, errors.New("find search document: invalid arguments")
	}
	var document Document
	err := store.database.QueryRowContext(ctx, `
		SELECT id, title, content, created_at, updated_at
		FROM posts
		WHERE id = ?`, postID).Scan(&document.PostID, &document.Title, &document.Content, &document.CreatedAt, &document.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, sql.ErrNoRows
	}
	if err != nil {
		return Document{}, fmt.Errorf("find search document: %w", err)
	}
	if err := document.Validate(); err != nil {
		return Document{}, fmt.Errorf("find search document: %w", err)
	}
	return document, nil
}

type DocumentIndexer interface {
	IndexAlias(context.Context, Document) error
}

type Processor struct {
	store   DocumentStore
	indexer DocumentIndexer
}

func NewProcessor(store DocumentStore, indexer DocumentIndexer) (*Processor, error) {
	if store == nil || indexer == nil {
		return nil, errors.New("search processor requires store and indexer")
	}
	return &Processor{store: store, indexer: indexer}, nil
}

func (processor *Processor) Process(ctx context.Context, envelope bus.Envelope) error {
	if envelope.EventType != bus.PostCreated {
		return worker.NewPermanentError("unsupported_event_type")
	}
	document, err := processor.store.FindDocument(ctx, envelope.PostID)
	if errors.Is(err, sql.ErrNoRows) {
		return worker.NewPermanentError("post_not_found")
	}
	if err != nil {
		return err
	}
	if err := processor.indexer.IndexAlias(ctx, document); err != nil {
		var permanent *PermanentIndexError
		if errors.As(err, &permanent) {
			return worker.NewPermanentError(permanent.Reason)
		}
		return err
	}
	return nil
}

type PermanentIndexError struct{ Reason string }

func (err *PermanentIndexError) Error() string { return err.Reason }

func (repository *ElasticsearchRepository) IndexAlias(ctx context.Context, document Document) error {
	if err := document.Validate(); err != nil {
		return &PermanentIndexError{Reason: "invalid_document"}
	}
	body, err := json.Marshal(document)
	if err != nil {
		return &PermanentIndexError{Reason: "invalid_document"}
	}
	path := "/" + url.PathEscape(AliasName) + "/_doc/" + strconv.FormatUint(document.PostID, 10) + "?require_alias=true"
	response, err := repository.do(ctx, http.MethodPut, path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusNotFound || response.StatusCode >= 500 {
		return ErrUnavailable
	}
	if response.StatusCode >= 400 && response.StatusCode < 500 {
		return &PermanentIndexError{Reason: "index_mapping_rejected"}
	}
	return ErrUnavailable
}
