package search

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/businessstate"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/tracing"
	"github.com/Ray-ymq/GoPulse/backend/internal/worker"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
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
	store         DocumentStore
	indexer       DocumentIndexer
	logger        *slog.Logger
	businessState *businessstate.Repository
}

func NewProcessor(store DocumentStore, indexer DocumentIndexer) (*Processor, error) {
	if store == nil || indexer == nil {
		return nil, errors.New("search processor requires store and indexer")
	}
	return &Processor{store: store, indexer: indexer, logger: logging.Module(logging.Discard("search-indexer"), "search")}, nil
}

// WithLogger attaches the process logger used for bounded index-stage records.
func (processor *Processor) WithLogger(logger *slog.Logger) *Processor {
	if logger != nil {
		processor.logger = logging.Module(logger, "search")
	}
	return processor
}

func (processor *Processor) WithBusinessState(repository *businessstate.Repository) *Processor {
	if processor != nil {
		processor.businessState = repository
	}
	return processor
}

func (processor *Processor) Process(ctx context.Context, envelope bus.Envelope) error {
	traceContext, span := tracing.Start(ctx, "search.process",
		attribute.String("gopulse.event_id", envelope.EventID),
		attribute.String("gopulse.event_type", string(envelope.EventType)),
		attribute.String("gopulse.data_epoch", envelope.DataEpoch),
		attribute.String("gopulse.command_id", envelope.CommandID),
		attribute.String("gopulse.routing_contract_revision", businessstate.SearchRoutingContractV1),
		attribute.String("gopulse.consumer", businessstate.ConsumerSearch),
		attribute.Int64("gopulse.post_id", int64(envelope.PostID)),
		attribute.Int64("gopulse.content_revision", int64(envelope.ContentRevision)),
		attribute.Int64("gopulse.outbox_id", int64(tracing.OutboxID(ctx))),
	)
	defer span.End()
	ctx = traceContext
	if envelope.EventType != bus.PostCreated && envelope.EventType != bus.PostUpdated && envelope.EventType != bus.PostDeleted {
		span.SetStatus(codes.Error, "unsupported_event_type")
		return worker.NewPermanentError("unsupported_event_type")
	}
	if processor.businessState != nil && envelope.SchemaVersion == bus.SchemaVersionV2 {
		environment, err := processor.businessState.Current(ctx)
		if err != nil {
			span.SetStatus(codes.Error, "business_state_unavailable")
			return err
		}
		if environment.Status != businessstate.StatusReady || environment.DataEpoch != envelope.DataEpoch {
			span.SetStatus(codes.Error, "stale_epoch")
			return worker.NewPermanentError("stale_epoch")
		}
		if generation, generationErr := processor.resolveGeneration(ctx, envelope.DataEpoch); generationErr == nil {
			exists, applicationErr := processor.businessState.FindApplication(ctx, businessstate.Application{
				DataEpoch: envelope.DataEpoch, EventID: envelope.EventID, Consumer: businessstate.ConsumerSearch,
				RoutingContractRevision: businessstate.SearchRoutingContractV1, ProjectionGeneration: generation,
			})
			if applicationErr != nil {
				return applicationErr
			}
			if exists {
				return nil
			}
		}
	}
	if store, ok := processor.store.(*MySQLDocumentStore); ok {
		// Hold the authoritative row lock through the external write. Deletion cannot
		// commit before an in-flight old snapshot has finished indexing.
		tx, err := store.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		componentmetrics.Dependency("mysql", err)
		if err != nil {
			span.SetStatus(codes.Error, "mysql_unavailable")
			return err
		}
		defer func() { _ = tx.Rollback() }()
		var id uint64
		err = tx.QueryRowContext(ctx, "SELECT id FROM posts WHERE id=? FOR UPDATE", envelope.PostID).Scan(&id)
		observedErr := err
		if errors.Is(err, sql.ErrNoRows) {
			observedErr = nil
		}
		componentmetrics.Dependency("mysql", observedErr)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			span.SetStatus(codes.Error, "mysql_unavailable")
			return err
		}
		if err = processor.project(ctx, envelope); err != nil {
			span.SetStatus(codes.Error, "projection_failed")
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
			indexStarted := time.Now()
			indexContext, span := tracing.Start(ctx, "search.index.delete", attribute.Int64("gopulse.post_id", int64(envelope.PostID)))
			defer span.End()
			var deleteResult DeleteResult
			if resultDeleter, resultOK := processor.indexer.(interface {
				DeleteAliasResult(context.Context, uint64) (DeleteResult, error)
			}); resultOK {
				deleteResult, err = resultDeleter.DeleteAliasResult(indexContext, envelope.PostID)
			} else {
				err = deleter.DeleteAlias(indexContext, envelope.PostID)
			}
			freshnessResult := "success"
			if err != nil {
				freshnessResult = "failure"
				span.SetStatus(codes.Error, "delete_failed")
			}
			componentmetrics.Active().Observe("freshness_events_total", time.Since(indexStarted), "index", freshnessResult)
			if err != nil {
				return err
			}
			if processor.businessState != nil && envelope.SchemaVersion == bus.SchemaVersionV2 {
				generation := deleteResult.Index
				if generation == "" {
					generation, err = processor.resolveGeneration(ctx, envelope.DataEpoch)
					if err != nil {
						return err
					}
				} else if verifier, ok := processor.indexer.(interface {
					IndexEpoch(context.Context, string) (string, error)
				}); ok {
					actualEpoch, verifyErr := verifier.IndexEpoch(ctx, generation)
					if verifyErr != nil || actualEpoch != envelope.DataEpoch {
						return ErrUnavailable
					}
				}
				return processor.recordApplication(ctx, envelope, generation, "no_effect_resource_absent", 0)
			}
			return nil
		}
		return errors.New("search indexer cannot delete missing fact")
	}
	if err != nil {
		return err
	}
	indexStarted := time.Now()
	indexContext, span := tracing.Start(ctx, "search.index", attribute.String("gopulse.event_id", envelope.EventID), attribute.Int64("gopulse.post_id", int64(envelope.PostID)), attribute.Int64("gopulse.content_revision", int64(document.ContentRevision)), attribute.Int64("gopulse.outbox_id", int64(tracing.OutboxID(ctx))))
	defer span.End()
	if envelope.SchemaVersion == bus.SchemaVersionV2 {
		document.DataEpoch = envelope.DataEpoch
	}
	var writeResult IndexWriteResult
	var indexErr error
	if resultIndexer, ok := processor.indexer.(interface {
		IndexAliasResult(context.Context, Document) (IndexWriteResult, error)
	}); ok {
		writeResult, indexErr = resultIndexer.IndexAliasResult(indexContext, document)
	} else {
		indexErr = processor.indexer.IndexAlias(indexContext, document)
	}
	if indexErr != nil {
		processor.logIndex(indexContext, envelope, "failure")
		componentmetrics.Active().Observe("freshness_events_total", time.Since(indexStarted), "index", "failure")
		span.SetStatus(codes.Error, "index_failed")
		var permanent *PermanentIndexError
		if errors.As(indexErr, &permanent) {
			return worker.NewPermanentError(permanent.Reason)
		}
		return indexErr
	}
	if envelope.SchemaVersion == bus.SchemaVersionV2 {
		if writeResult.Conflict && !writeResult.ConflictVerified {
			return ErrUnavailable
		}
		if writeResult.Index == "" || writeResult.Epoch != envelope.DataEpoch {
			return ErrUnavailable
		}
		if err := processor.recordApplication(ctx, envelope, writeResult.Index, "applied", writeResult.Version); err != nil {
			return err
		}
	}
	processor.logIndex(indexContext, envelope, "success", writeResult.Index)
	componentmetrics.Active().Observe("freshness_events_total", time.Since(indexStarted), "index", "success")
	return nil
}

func (processor *Processor) resolveGeneration(ctx context.Context, epoch string) (string, error) {
	if resolver, ok := processor.indexer.(interface {
		ResolveGenerationForEpoch(context.Context, string) (string, error)
	}); ok {
		return resolver.ResolveGenerationForEpoch(ctx, epoch)
	}
	if resolver, ok := processor.indexer.(interface {
		ResolveGeneration(context.Context) (string, error)
	}); ok {
		return resolver.ResolveGeneration(ctx)
	}
	return "", ErrUnavailable
}

func (processor *Processor) recordApplication(ctx context.Context, envelope bus.Envelope, generation, result string, version uint64) error {
	application := businessstate.Application{
		DataEpoch: envelope.DataEpoch, EventID: envelope.EventID, Consumer: businessstate.ConsumerSearch,
		RoutingContractRevision: businessstate.SearchRoutingContractV1, ProjectionGeneration: generation,
		Result: result, ActualVersion: version,
	}
	if err := processor.businessState.RecordApplication(ctx, application); err != nil {
		processor.logger.Error("search application result failed", slog.String("event_id", envelope.EventID), slog.String("data_epoch", envelope.DataEpoch), slog.String("consumer", businessstate.ConsumerSearch), slog.String("routing_contract_revision", businessstate.SearchRoutingContractV1), slog.String("result", result), slog.String("projection_generation", generation), slog.String("reason", "application_result_write_failed"))
		return err
	}
	return nil
}

func (processor *Processor) logIndex(ctx context.Context, envelope bus.Envelope, result string, generations ...string) {
	generation := ""
	if len(generations) > 0 {
		generation = generations[0]
	}
	attributes := []any{
		slog.String("event_id", envelope.EventID),
		slog.String("event_type", string(envelope.EventType)),
		slog.Uint64("post_id", envelope.PostID),
		slog.Uint64("content_revision", envelope.ContentRevision),
		slog.Uint64("outbox_id", tracing.OutboxID(ctx)),
		slog.String("attempt_id", tracing.AttemptID(ctx)),
		slog.String("consumer", businessstate.ConsumerSearch),
		slog.String("routing_contract_revision", businessstate.SearchRoutingContractV1),
		slog.String("stage", "index"),
		slog.String("result", result),
	}
	if envelope.DataEpoch != "" {
		attributes = append(attributes, slog.String("data_epoch", envelope.DataEpoch))
	}
	if envelope.CommandID != "" {
		attributes = append(attributes, slog.String("command_id", envelope.CommandID))
	}
	if generation != "" {
		attributes = append(attributes, slog.String("projection_generation", generation))
	}
	logging.WithTrace(processor.logger, ctx).Info("event processed", attributes...)
}

type PermanentIndexError struct{ Reason string }

func (err *PermanentIndexError) Error() string { return err.Reason }

func (repository *ElasticsearchRepository) IndexAlias(ctx context.Context, document Document) error {
	_, err := repository.IndexAliasResult(ctx, document)
	return err
}

func (repository *ElasticsearchRepository) IndexAliasResult(ctx context.Context, document Document) (IndexWriteResult, error) {
	if err := document.Validate(); err != nil {
		return IndexWriteResult{}, &PermanentIndexError{Reason: "invalid_document"}
	}
	body, err := json.Marshal(document)
	if err != nil {
		return IndexWriteResult{}, &PermanentIndexError{Reason: "invalid_document"}
	}
	path := "/" + url.PathEscape(AliasName) + "/_doc/" + strconv.FormatUint(document.PostID, 10) + "?require_alias=true"
	if document.ContentRevision > 0 {
		path += "&version_type=external_gte&version=" + strconv.FormatUint(document.ContentRevision, 10)
	}
	response, err := repository.do(ctx, http.MethodPut, path, bytes.NewReader(body))
	if err != nil {
		return IndexWriteResult{}, err
	}
	defer func() { _ = response.Body.Close() }()
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes+1))
	if readErr != nil || len(raw) > maximumResponseBytes {
		return IndexWriteResult{}, ErrUnavailable
	}
	result := parseIndexWriteResult(raw)
	if response.StatusCode == http.StatusConflict {
		if document.DataEpoch == "" {
			// Legacy v1 keeps the historical external_gte conflict semantics.
			return IndexWriteResult{Conflict: true, ConflictVerified: true, Index: result.Index, DocumentID: result.DocumentID, Version: result.Version}, nil
		}
		if result.Index == "" || result.DocumentID != strconv.FormatUint(document.PostID, 10) || result.Version < document.ContentRevision {
			return IndexWriteResult{}, ErrUnavailable
		}
		if err := repository.verifyStoredDocument(ctx, result, document); err != nil {
			return IndexWriteResult{}, err
		}
		return IndexWriteResult{Conflict: true, ConflictVerified: true, Index: result.Index, DocumentID: result.DocumentID, Version: result.Version, Epoch: document.DataEpoch}, nil
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		if document.DataEpoch == "" && result.Index == "" {
			return result, nil
		}
		if result.Index == "" || result.DocumentID != strconv.FormatUint(document.PostID, 10) || result.Version == 0 {
			return IndexWriteResult{}, ErrUnavailable
		}
		if document.DataEpoch != "" {
			if err := repository.verifyIndexEpoch(ctx, result.Index, document.DataEpoch); err != nil {
				return IndexWriteResult{}, err
			}
			result.Epoch = document.DataEpoch
		}
		return result, nil
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusNotFound || response.StatusCode >= 500 {
		return IndexWriteResult{}, ErrUnavailable
	}
	if response.StatusCode >= 400 && response.StatusCode < 500 {
		return IndexWriteResult{}, &PermanentIndexError{Reason: "index_mapping_rejected"}
	}
	return IndexWriteResult{}, ErrUnavailable
}

func (repository *ElasticsearchRepository) IndexAliasWithResult(ctx context.Context, document Document) (IndexWriteResult, error) {
	return repository.IndexAliasResult(ctx, document)
}

func (repository *ElasticsearchRepository) verifyIndexEpoch(ctx context.Context, index, epoch string) error {
	actual, err := repository.IndexEpoch(ctx, index)
	if err != nil || actual != epoch {
		return ErrUnavailable
	}
	return nil
}

func (repository *ElasticsearchRepository) verifyStoredDocument(ctx context.Context, result IndexWriteResult, expected Document) error {
	if err := repository.verifyIndexEpoch(ctx, result.Index, expected.DataEpoch); err != nil {
		return err
	}
	response, err := repository.do(ctx, http.MethodGet, "/"+url.PathEscape(result.Index)+"/_doc/"+url.PathEscape(result.DocumentID), nil)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ErrUnavailable
	}
	var payload struct {
		Index   string          `json:"_index"`
		ID      string          `json:"_id"`
		Version uint64          `json:"_version"`
		Source  json.RawMessage `json:"_source"`
	}
	if err := decodeJSON(response.Body, &payload); err != nil || payload.Index != result.Index || payload.ID != result.DocumentID || payload.Version < expected.ContentRevision {
		return ErrUnavailable
	}
	var actual Document
	if json.Unmarshal(payload.Source, &actual) != nil || actual.DataEpoch != expected.DataEpoch || actual.PostID != expected.PostID || actual.Title != expected.Title || actual.Content != expected.Content || actual.ContentRevision < expected.ContentRevision || !actual.CreatedAt.Equal(expected.CreatedAt) || !actual.UpdatedAt.Equal(expected.UpdatedAt) {
		return ErrUnavailable
	}
	return nil
}

func parseIndexWriteResult(raw []byte) IndexWriteResult {
	var payload struct {
		Index   string `json:"_index"`
		ID      string `json:"_id"`
		Version uint64 `json:"_version"`
		Result  string `json:"result"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return IndexWriteResult{}
	}
	return IndexWriteResult{Index: payload.Index, DocumentID: payload.ID, Version: payload.Version}
}

// Missing authoritative facts remove the projection idempotently.
func (repository *ElasticsearchRepository) DeleteAlias(ctx context.Context, id uint64) error {
	_, err := repository.DeleteAliasResult(ctx, id)
	return err
}

func (repository *ElasticsearchRepository) DeleteAliasResult(ctx context.Context, id uint64) (DeleteResult, error) {
	response, err := repository.do(ctx, http.MethodDelete, "/"+url.PathEscape(AliasName)+"/_doc/"+strconv.FormatUint(id, 10), nil)
	if err != nil {
		return DeleteResult{}, err
	}
	defer func() { _ = response.Body.Close() }()
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes+1))
	if readErr != nil || len(raw) > maximumResponseBytes {
		return DeleteResult{}, ErrUnavailable
	}
	if response.StatusCode == http.StatusNotFound {
		return DeleteResult{DocumentID: strconv.FormatUint(id, 10), Found: false}, nil
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		result := parseDeleteResult(raw)
		if result.DocumentID == "" {
			result.DocumentID = strconv.FormatUint(id, 10)
		}
		result.Found = true
		return result, nil
	}
	return DeleteResult{}, ErrUnavailable
}

func parseDeleteResult(raw []byte) DeleteResult {
	var payload struct {
		Index string `json:"_index"`
		ID    string `json:"_id"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return DeleteResult{}
	}
	return DeleteResult{Index: payload.Index, DocumentID: payload.ID}
}
