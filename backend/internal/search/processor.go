package search

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
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
		SELECT id, title, content, created_at, updated_at, edited_at, content_revision
		FROM posts
		WHERE id = ?`, postID).Scan(&document.PostID, &document.Title, &document.Content, &document.CreatedAt, &document.UpdatedAt, &document.EditedAt, &document.ContentRevision)
	observedErr := err
	if errors.Is(err, sql.ErrNoRows) {
		observedErr = nil
	}
	componentmetrics.Dependency("mysql", observedErr)
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
	if envelope.EventType != bus.PostCreated && envelope.EventType != bus.PostUpdated && envelope.EventType != bus.PostDeleted {
		return worker.NewPermanentError("unsupported_event_type")
	}
	if store, ok := processor.store.(*MySQLDocumentStore); ok {
		// Hold the authoritative row lock through the external write. Deletion cannot
		// commit before an in-flight old snapshot has finished indexing.
		tx, err := store.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		componentmetrics.Dependency("mysql", err)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var id uint64
		err = tx.QueryRowContext(ctx, "SELECT id FROM posts WHERE id=? FOR UPDATE", envelope.PostID).Scan(&id)
		observedErr := err
		if errors.Is(err, sql.ErrNoRows) {
			observedErr = nil
		}
		componentmetrics.Dependency("mysql", observedErr)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err = processor.project(ctx, envelope); err != nil {
			return err
		}
		err = tx.Commit()
		componentmetrics.Dependency("mysql", err)
		return err
	}
	return processor.project(ctx, envelope)
}

func (processor *Processor) project(ctx context.Context, envelope bus.Envelope) error {
	document, err := processor.store.FindDocument(ctx, envelope.PostID)
	if errors.Is(err, sql.ErrNoRows) {
		if deleter, ok := processor.indexer.(interface {
			DeleteAlias(context.Context, uint64) error
		}); ok {
			return deleter.DeleteAlias(ctx, envelope.PostID)
		}
		return errors.New("search indexer cannot delete missing fact")
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
	if document.ContentRevision > 0 {
		path += "&version_type=external_gte&version=" + strconv.FormatUint(document.ContentRevision, 10)
	}
	response, err := repository.do(ctx, http.MethodPut, path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusConflict || (response.StatusCode >= 200 && response.StatusCode < 300) {
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

// Missing authoritative facts remove the projection idempotently.
func (repository *ElasticsearchRepository) DeleteAlias(ctx context.Context, id uint64) error {
	response, err := repository.do(ctx, http.MethodDelete, "/"+url.PathEscape(AliasName)+"/_doc/"+strconv.FormatUint(id, 10), nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound || response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	return ErrUnavailable
}
