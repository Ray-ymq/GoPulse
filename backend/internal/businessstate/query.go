package businessstate

// This file contains the bounded, read-only business-stage query.  It lives
// beside the durable state repository so the business role owns the database
// reads, while the platform role can only use the narrow HTTP client defined
// below.  The query deliberately returns identities and evidence summaries;
// it never returns command digests, user supplied post content, or payloads.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
)

const (
	StageDBTimeout       = 500 * time.Millisecond
	StageBusinessTimeout = 2 * time.Second
	StagePlatformTimeout = 3 * time.Second
	StageMaximumLogs     = 100
	StageMaximumSources  = 2
	StageMaximumEvidence = 20
)

var (
	ErrStageBusy         = errors.New("business stage query is busy")
	ErrStageBackend      = errors.New("business stage backend unavailable")
	ErrStageUnauthorized = errors.New("business stage authorization failed")
	ErrStageRemote       = errors.New("business stage private endpoint unavailable")
	ErrStageResponseSize = errors.New("business stage response is too large")
	ErrFactNotFound      = errors.New("business post fact not found")
)

const (
	StageQualityKnown         = "known"
	StageQualityNoEvidence    = "no_evidence"
	StageQualityLegacyUnknown = "legacy_unknown"
	StageQualityStaleEpoch    = "stale_epoch"
	StageQualityExpired       = "expired"
	StageQualityNotRequested  = "not_requested"
	StageQualityNotConfigured = "not_configured"
	StageQualityBackendError  = "backend_error"
	StageQualitySampling      = "sampling"
	StageSourceCommand        = "command"
	StageSourceBroker         = "broker"
	StageSourceApplication    = "application"
	StageSourceVisibility     = "search_visibility"
	StageSourceLogs           = "logs"
	StageSourceTrace          = "trace"
	StageConsumerSearch       = ConsumerSearch
)

type StageQuery struct {
	DataEpoch       string
	CommandID       string
	EventID         string
	PostID          uint64
	CheckVisibility bool
	LogSources      []string
	LogFrom         time.Time
	LogTo           time.Time
}

func (query StageQuery) Validate() error {
	if err := ValidateEpoch(query.DataEpoch); err != nil {
		return err
	}
	selectors := 0
	if query.CommandID != "" {
		selectors++
		if err := validateEventID(query.CommandID); err != nil {
			return fmt.Errorf("%w: command_id is invalid", ErrInvalidArgument)
		}
	}
	if query.EventID != "" {
		selectors++
		if err := validateEventID(query.EventID); err != nil {
			return fmt.Errorf("%w: event_id is invalid", ErrInvalidArgument)
		}
	}
	if query.PostID != 0 {
		selectors++
	}
	if selectors != 1 {
		return fmt.Errorf("%w: exactly one business object selector is required", ErrInvalidArgument)
	}
	if len(query.LogSources) > StageMaximumSources {
		return fmt.Errorf("%w: too many evidence sources", ErrInvalidArgument)
	}
	seen := make(map[string]bool, len(query.LogSources))
	for _, source := range query.LogSources {
		if source != StageSourceLogs && source != StageSourceTrace || seen[source] {
			return fmt.Errorf("%w: evidence source is invalid", ErrInvalidArgument)
		}
		seen[source] = true
	}
	if !query.LogFrom.IsZero() || !query.LogTo.IsZero() {
		if query.LogFrom.IsZero() || query.LogTo.IsZero() || !query.LogFrom.Before(query.LogTo) || query.LogTo.Sub(query.LogFrom) > 24*time.Hour || query.LogFrom.Location() != time.UTC || query.LogTo.Location() != time.UTC {
			return fmt.Errorf("%w: evidence time window is invalid", ErrInvalidArgument)
		}
	}
	return nil
}

type StageResolved struct {
	CommandID string `json:"command_id,omitempty"`
	EventID   string `json:"event_id,omitempty"`
	PostID    uint64 `json:"post_id,omitempty"`
}

type StageSource struct {
	Source                  string          `json:"source"`
	Quality                 string          `json:"quality"`
	ReadAt                  *time.Time      `json:"read_at,omitempty"`
	DataEpoch               string          `json:"data_epoch,omitempty"`
	CommandID               string          `json:"command_id,omitempty"`
	EventID                 string          `json:"event_id,omitempty"`
	Consumer                string          `json:"consumer,omitempty"`
	RoutingContractRevision string          `json:"routing_contract_revision,omitempty"`
	ProjectionGeneration    string          `json:"projection_generation,omitempty"`
	Status                  string          `json:"status,omitempty"`
	Result                  string          `json:"result,omitempty"`
	Reason                  string          `json:"reason,omitempty"`
	ActualVersion           uint64          `json:"actual_version,omitempty"`
	Matched                 *bool           `json:"matched,omitempty"`
	Entries                 []StageLogEntry `json:"entries,omitempty"`
	Truncated               bool            `json:"truncated,omitempty"`
	TraceIDs                []string        `json:"trace_ids,omitempty"`
	Sampling                string          `json:"sampling,omitempty"`
}

type StageLogEntry struct {
	Timestamp  string `json:"timestamp"`
	Level      string `json:"level,omitempty"`
	Service    string `json:"service,omitempty"`
	InstanceID string `json:"instance_id,omitempty"`
	Module     string `json:"module,omitempty"`
	Message    string `json:"message,omitempty"`
	TraceID    string `json:"trace_id,omitempty"`
	SpanID     string `json:"span_id,omitempty"`
	AttemptID  string `json:"attempt_id,omitempty"`
	EventID    string `json:"event_id,omitempty"`
	CommandID  string `json:"command_id,omitempty"`
	DataEpoch  string `json:"data_epoch,omitempty"`
	PostID     uint64 `json:"post_id,omitempty"`
}

type StageConsumer struct {
	Consumer                string        `json:"consumer"`
	RoutingContractRevision string        `json:"routing_contract_revision"`
	ApplicableObjectVersion uint64        `json:"applicable_object_version,omitempty"`
	Quality                 string        `json:"quality"`
	Status                  string        `json:"status,omitempty"`
	Result                  string        `json:"result,omitempty"`
	ProjectionGeneration    string        `json:"projection_generation,omitempty"`
	ActualVersion           uint64        `json:"actual_version,omitempty"`
	CompletedAt             *time.Time    `json:"completed_at,omitempty"`
	RetainUntil             *time.Time    `json:"retain_until,omitempty"`
	Failure                 *StageFailure `json:"failure,omitempty"`
}

type StageFailure struct {
	AttemptCount uint64 `json:"attempt_count,omitempty"`
	RetryCount   uint64 `json:"retry_count,omitempty"`
	DeadCount    uint64 `json:"dead_count,omitempty"`
	LastReason   string `json:"last_reason,omitempty"`
	RetrySummary string `json:"retry_summary,omitempty"`
	DeadSummary  string `json:"dead_summary,omitempty"`
}

type StageEvidence struct {
	Command    StageSource     `json:"command"`
	Broker     StageSource     `json:"broker"`
	Consumers  []StageConsumer `json:"consumers,omitempty"`
	Visibility StageSource     `json:"visibility"`
	Logs       StageSource     `json:"logs"`
	Trace      StageSource     `json:"trace"`
}

type StageBudget struct {
	CurrentOpenRequirements uint64 `json:"current_open_requirements"`
	CurrentOpenLimit        uint64 `json:"current_open_limit"`
	RetainedGroups          uint64 `json:"retained_groups"`
	RetainedGroupsLimit     uint64 `json:"retained_groups_limit"`
	RetainedBytes           uint64 `json:"retained_bytes"`
	RetainedBytesLimit      uint64 `json:"retained_bytes_limit"`
	AtEightyPercent         bool   `json:"at_eighty_percent"`
}

type StageResponse struct {
	RequestedEpoch          string        `json:"requested_epoch"`
	CurrentEpoch            string        `json:"current_epoch"`
	Resolved                StageResolved `json:"resolved"`
	RoutingContractRevision string        `json:"routing_contract_revision,omitempty"`
	Evidence                StageEvidence `json:"evidence"`
	Budget                  StageBudget   `json:"budget"`
}

type CurrentPost struct {
	PostID          uint64
	ContentRevision uint64
}

type FactReader interface {
	ReadCurrent(context.Context, uint64) (CurrentPost, error)
}

type PostFactReader struct{ database *sql.DB }

func NewPostFactReader(database *sql.DB) (*PostFactReader, error) {
	if database == nil {
		return nil, fmt.Errorf("%w: post fact database is required", ErrInvalidArgument)
	}
	return &PostFactReader{database: database}, nil
}

func (reader *PostFactReader) ReadCurrent(ctx context.Context, postID uint64) (CurrentPost, error) {
	if reader == nil || reader.database == nil || postID == 0 {
		return CurrentPost{}, fmt.Errorf("%w: post fact lookup is invalid", ErrInvalidArgument)
	}
	var fact CurrentPost
	if err := reader.database.QueryRowContext(ctx, `SELECT id, content_revision FROM posts WHERE id=?`, postID).Scan(&fact.PostID, &fact.ContentRevision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CurrentPost{}, ErrFactNotFound
		}
		return CurrentPost{}, ErrStageBackend
	}
	return fact, nil
}

type VisibilityResult struct {
	Matched    bool
	Epoch      string
	Generation string
	Version    uint64
	ReadAt     time.Time
}

type VisibilityChecker interface {
	CheckVisibility(context.Context, string, uint64, uint64) (VisibilityResult, error)
}

type LogCorrelationQuery struct {
	DataEpoch string
	CommandID string
	EventID   string
	PostID    uint64
	From      time.Time
	To        time.Time
	Limit     int
}

type LogCorrelationEntry struct {
	Timestamp  string
	Level      string
	Service    string
	InstanceID string
	Module     string
	Message    string
	TraceID    string
	SpanID     string
	AttemptID  string
	EventID    string
	CommandID  string
	DataEpoch  string
	PostID     uint64
}

type LogCorrelationResult struct {
	Entries   []LogCorrelationEntry
	Truncated bool
	Quality   string
}

type LogReader interface {
	QueryCorrelation(context.Context, LogCorrelationQuery) (LogCorrelationResult, error)
}

type QueryService struct {
	repository      *Repository
	facts           FactReader
	visibility      VisibilityChecker
	logs            LogReader
	traceConfigured bool
	slots           chan struct{}
	now             func() time.Time
}

func NewQueryService(repository *Repository, facts FactReader, visibility VisibilityChecker, logs LogReader, traceConfigured bool) (*QueryService, error) {
	if repository == nil || repository.database == nil {
		return nil, fmt.Errorf("%w: business stage repository is required", ErrInvalidArgument)
	}
	return &QueryService{repository: repository, facts: facts, visibility: visibility, logs: logs, traceConfigured: traceConfigured, slots: make(chan struct{}, 2), now: time.Now}, nil
}

func (service *QueryService) WithClock(clock func() time.Time) *QueryService {
	if service != nil && clock != nil {
		service.now = clock
	}
	return service
}

func (service *QueryService) WithLogReader(reader LogReader) *QueryService {
	if service != nil {
		service.logs = reader
	}
	return service
}

func (service *QueryService) Query(ctx context.Context, query StageQuery) (StageResponse, error) {
	if service == nil || service.repository == nil || ctx == nil {
		return StageResponse{}, ErrStageBackend
	}
	if err := query.Validate(); err != nil {
		return StageResponse{}, err
	}
	select {
	case service.slots <- struct{}{}:
		defer func() { <-service.slots }()
	default:
		return StageResponse{}, ErrStageBusy
	}
	if err := ctx.Err(); err != nil {
		return StageResponse{}, err
	}
	now := service.now().UTC()
	dbContext, cancel := context.WithTimeout(ctx, StageDBTimeout)
	persistent, err := service.readPersistent(dbContext, query, now)
	cancel()
	if err != nil {
		return StageResponse{}, err
	}
	response := persistent.response
	if query.CheckVisibility {
		service.readVisibility(ctx, query, persistent, &response, now)
	}
	if hasSource(query.LogSources, StageSourceLogs) || hasSource(query.LogSources, StageSourceTrace) {
		service.readLogs(ctx, query, persistent, &response, now)
	}
	return response, nil
}

type persistentStage struct {
	response     StageResponse
	command      Command
	commandFound bool
	epochBound   bool
	postID       uint64
	eventID      string
}

func (service *QueryService) readPersistent(ctx context.Context, query StageQuery, now time.Time) (persistentStage, error) {
	environment, err := service.repository.Current(ctx)
	if err != nil {
		return persistentStage{}, ErrStageBackend
	}
	readAt := now
	response := StageResponse{RequestedEpoch: query.DataEpoch, CurrentEpoch: environment.DataEpoch, Evidence: StageEvidence{
		Command: source(StageSourceCommand, readAt), Broker: source(StageSourceBroker, readAt),
		Visibility: source(StageSourceVisibility, readAt), Logs: source(StageSourceLogs, readAt), Trace: source(StageSourceTrace, readAt),
	}, Budget: StageBudget{CurrentOpenLimit: MaximumOpenRequirements, RetainedGroupsLimit: MaximumRetainedGroups, RetainedBytesLimit: MaximumRetainedBytes}}
	response.Evidence.Visibility.Quality = StageQualityNotRequested
	response.Evidence.Logs.Quality = StageQualityNotRequested
	response.Evidence.Trace.Quality = StageQualityNotRequested

	// Open requirements are a current-epoch operating signal; retained totals
	// span historical epochs and are read separately below.
	budget, err := service.readBudget(ctx, environment.DataEpoch)
	if err != nil {
		return persistentStage{}, ErrStageBackend
	}
	response.Budget.CurrentOpenRequirements = budget.currentOpen
	response.Budget.RetainedGroups = budget.retainedGroups
	response.Budget.RetainedBytes = budget.retainedBytes
	response.Budget.AtEightyPercent = budget.currentOpen*100 >= MaximumOpenRequirements*80 || budget.retainedGroups*100 >= MaximumRetainedGroups*80 || budget.retainedBytes*100 >= MaximumRetainedBytes*80

	stage := persistentStage{response: response}
	command, found, err := service.findCommand(ctx, query)
	if err != nil {
		return persistentStage{}, ErrStageBackend
	}
	stage.command, stage.commandFound = command, found
	if found {
		stage.postID, stage.eventID = command.PostID, command.EventID
		stage.epochBound = true
		stage.response.Resolved = StageResolved{CommandID: command.CommandID, EventID: command.EventID, PostID: command.PostID}
		stage.response.Evidence.Command = commandEvidence(command, environment.DataEpoch, now)
		stage.response.RoutingContractRevision = SearchRoutingContractV1
	} else {
		stage.response.Evidence.Command.Quality = absentQuality(query.DataEpoch, environment.DataEpoch)
	}

	if query.EventID != "" || stage.eventID != "" {
		if query.EventID != "" {
			stage.eventID = query.EventID
		}
		if stage.response.Resolved.EventID == "" {
			stage.response.Resolved.EventID = stage.eventID
		}
		broker, resolved, brokerErr := service.findBroker(ctx, stage.eventID, query.DataEpoch)
		if brokerErr != nil {
			return persistentStage{}, ErrStageBackend
		}
		if resolved.EventID != "" {
			if resolved.DataEpoch == query.DataEpoch {
				stage.epochBound = true
			}
			stage.response.Resolved.EventID = resolved.EventID
			if stage.response.Resolved.PostID == 0 {
				stage.response.Resolved.PostID = resolved.PostID
				stage.postID = resolved.PostID
			}
			if stage.response.Resolved.CommandID == "" {
				stage.response.Resolved.CommandID = resolved.CommandID
			}
		}
		if broker.Status != "" {
			stage.response.Evidence.Broker = brokerEvidence(broker, query.DataEpoch, environment.DataEpoch, now)
		} else {
			stage.response.Evidence.Broker.Quality = absentQuality(query.DataEpoch, environment.DataEpoch)
		}
	}
	if stage.response.Resolved.EventID != "" {
		consumers, err := service.readConsumers(ctx, query.DataEpoch, stage.response.Resolved.EventID, environment.DataEpoch, now)
		if err != nil {
			return persistentStage{}, ErrStageBackend
		}
		stage.response.Evidence.Consumers = consumers
	}
	if stage.response.Resolved.EventID != "" && stage.response.Resolved.CommandID == "" {
		stage.response.Evidence.Command.Quality = absentQuality(query.DataEpoch, environment.DataEpoch)
	}
	return stage, nil
}

type budgetSnapshot struct{ currentOpen, retainedGroups, retainedBytes uint64 }

func (service *QueryService) readBudget(ctx context.Context, epoch string) (budgetSnapshot, error) {
	var snapshot budgetSnapshot
	if err := service.repository.database.QueryRowContext(ctx, `SELECT open_requirements FROM business_async_budget WHERE data_epoch=?`, epoch).Scan(&snapshot.currentOpen); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return budgetSnapshot{}, err
	}
	if err := service.repository.database.QueryRowContext(ctx, `SELECT COALESCE(SUM(retained_groups),0), COALESCE(SUM(retained_bytes),0) FROM business_async_budget`).Scan(&snapshot.retainedGroups, &snapshot.retainedBytes); err != nil {
		return budgetSnapshot{}, err
	}
	return snapshot, nil
}

func (service *QueryService) findCommand(ctx context.Context, query StageQuery) (Command, bool, error) {
	where, value := "", any(nil)
	switch {
	case query.CommandID != "":
		where, value = "data_epoch=? AND command_id=?", query.CommandID
	case query.EventID != "":
		where, value = "data_epoch=? AND event_id=?", query.EventID
	default:
		where, value = "data_epoch=? AND post_id=? AND command_type=?", query.PostID
	}
	args := []any{query.DataEpoch, value}
	if query.PostID != 0 && query.CommandID == "" && query.EventID == "" {
		args = append(args, PostCreateCommandType)
	}
	var command Command
	var keyHash, digest []byte
	var committed, retained sql.NullTime
	err := service.repository.database.QueryRowContext(ctx, `SELECT data_epoch, command_id, user_id, command_type, idempotency_key_hash, request_digest, COALESCE(post_id,0), COALESCE(event_id,''), status, committed_at, retained_until FROM business_commands WHERE `+where+` LIMIT 1`, args...).Scan(&command.DataEpoch, &command.CommandID, &command.UserID, &command.CommandType, &keyHash, &digest, &command.PostID, &command.EventID, &command.Status, &committed, &retained)
	if errors.Is(err, sql.ErrNoRows) {
		return Command{}, false, nil
	}
	if err != nil {
		return Command{}, false, err
	}
	command.IdempotencyKeyHash = append([]byte(nil), keyHash...)
	command.RequestDigest = append([]byte(nil), digest...)
	if committed.Valid {
		command.CommittedAt = committed.Time
	}
	if retained.Valid {
		command.RetainedUntil = retained.Time
	}
	return command, true, nil
}

func (service *QueryService) findBroker(ctx context.Context, eventID, epoch string) (brokerRow, bus.Envelope, error) {
	var row brokerRow
	var payload []byte
	var published sql.NullTime
	var lastError sql.NullString
	err := service.repository.database.QueryRowContext(ctx, `SELECT event_id, event_type, schema_version, payload, status, attempt_count, published_at, last_error FROM business_outbox WHERE event_id=? LIMIT 1`, eventID).Scan(&row.EventID, &row.EventType, &row.SchemaVersion, &payload, &row.Status, &row.AttemptCount, &published, &lastError)
	if errors.Is(err, sql.ErrNoRows) {
		return brokerRow{}, bus.Envelope{}, nil
	}
	if err != nil {
		return brokerRow{}, bus.Envelope{}, err
	}
	if published.Valid {
		row.PublishedAt = published.Time
	}
	if lastError.Valid {
		row.LastError = safeReason(lastError.String)
	}
	envelope, decodeErr := bus.Decode(payload)
	if decodeErr != nil {
		return row, bus.Envelope{}, nil
	}
	if envelope.DataEpoch != "" && envelope.DataEpoch != epoch {
		return brokerRow{}, bus.Envelope{}, nil
	}
	row.DataEpoch = envelope.DataEpoch
	return row, envelope, nil
}

type brokerRow struct {
	EventID       string
	EventType     string
	SchemaVersion int
	DataEpoch     string
	Status        string
	AttemptCount  uint64
	PublishedAt   time.Time
	LastError     string
}

func (service *QueryService) readConsumers(ctx context.Context, epoch, eventID, currentEpoch string, now time.Time) ([]StageConsumer, error) {
	rows, err := service.repository.database.QueryContext(ctx, `SELECT r.consumer, r.routing_contract_revision, r.applicable_object_version, r.closed_at, r.retain_until, COALESCE(a.projection_generation,''), COALESCE(a.result,''), COALESCE(a.actual_version,0), a.completed_at, COALESCE(f.attempt_count,0), COALESCE(f.retry_count,0), COALESCE(f.dead_count,0), COALESCE(f.last_reason,''), COALESCE(f.retry_summary,''), COALESCE(f.dead_summary,'') FROM business_event_requirements r LEFT JOIN business_application_results a ON a.data_epoch=r.data_epoch AND a.event_id=r.event_id AND a.consumer=r.consumer AND a.routing_contract_revision=r.routing_contract_revision LEFT JOIN business_event_failures f ON f.data_epoch=r.data_epoch AND f.event_id=r.event_id AND f.consumer=r.consumer WHERE r.data_epoch=? AND r.event_id=? ORDER BY r.consumer, r.routing_contract_revision, a.completed_at DESC LIMIT ?`, epoch, eventID, StageMaximumEvidence+1)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	consumers := make([]StageConsumer, 0, StageMaximumEvidence)
	for rows.Next() {
		var consumer, revision string
		var applicable uint64
		var closed, retain, completed sql.NullTime
		var generation, result string
		var actual uint64
		var attempt, retry, dead uint64
		var lastReason, retrySummary, deadSummary string
		if err := rows.Scan(&consumer, &revision, &applicable, &closed, &retain, &generation, &result, &actual, &completed, &attempt, &retry, &dead, &lastReason, &retrySummary, &deadSummary); err != nil {
			return nil, err
		}
		quality := epochQuality(epoch, currentEpoch)
		item := StageConsumer{Consumer: consumer, RoutingContractRevision: revision, ApplicableObjectVersion: applicable, Quality: quality, ProjectionGeneration: generation, Result: result, ActualVersion: actual}
		if result != "" {
			item.Status = "completed"
		}
		if closed.Valid {
			item.RetainUntil = timePtr(retain.Time)
			item.CompletedAt = timePtr(completed.Time)
		}
		if attempt > 0 || retry > 0 || dead > 0 || lastReason != "" {
			item.Failure = &StageFailure{AttemptCount: attempt, RetryCount: retry, DeadCount: dead, LastReason: safeStageReason(lastReason), RetrySummary: safeStageReason(retrySummary), DeadSummary: safeStageReason(deadSummary)}
		}
		if item.Status == "" && item.Failure == nil {
			item.Status = "pending"
		}
		consumers = append(consumers, item)
		if len(consumers) == StageMaximumEvidence+1 {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(consumers) > StageMaximumEvidence {
		consumers = consumers[:StageMaximumEvidence]
	}
	return consumers, nil
}

func (service *QueryService) readVisibility(ctx context.Context, query StageQuery, persistent persistentStage, response *StageResponse, now time.Time) {
	response.Evidence.Visibility = source(StageSourceVisibility, now)
	if query.DataEpoch != response.CurrentEpoch {
		response.Evidence.Visibility.Quality = StageQualityStaleEpoch
		response.Evidence.Visibility.Reason = "requested_epoch_is_not_current"
		return
	}
	postID := response.Resolved.PostID
	if postID == 0 || !persistent.epochBound || service.facts == nil || service.visibility == nil {
		response.Evidence.Visibility.Quality = StageQualityNoEvidence
		response.Evidence.Visibility.Reason = "post_identity_unresolved"
		return
	}
	fact, err := service.facts.ReadCurrent(ctx, postID)
	if err != nil {
		if errors.Is(err, ErrFactNotFound) {
			response.Evidence.Visibility.Quality = StageQualityNoEvidence
			response.Evidence.Visibility.Reason = "current_fact_absent"
			return
		}
		response.Evidence.Visibility.Quality = StageQualityBackendError
		response.Evidence.Visibility.Reason = "current_fact_unavailable"
		return
	}
	result, err := service.visibility.CheckVisibility(ctx, query.DataEpoch, postID, fact.ContentRevision)
	if err != nil {
		if result.Epoch != "" && result.Epoch != query.DataEpoch {
			response.Evidence.Visibility.DataEpoch = result.Epoch
			response.Evidence.Visibility.ProjectionGeneration = result.Generation
			response.Evidence.Visibility.ReadAt = timePtr(result.ReadAt)
			response.Evidence.Visibility.Quality = StageQualityStaleEpoch
			response.Evidence.Visibility.Reason = "index_epoch_mismatch"
			return
		}
		response.Evidence.Visibility.Quality = StageQualityBackendError
		response.Evidence.Visibility.Reason = "search_visibility_unavailable"
		return
	}
	matched := result.Matched
	response.Evidence.Visibility.Matched = &matched
	response.Evidence.Visibility.DataEpoch = result.Epoch
	response.Evidence.Visibility.ProjectionGeneration = result.Generation
	response.Evidence.Visibility.ActualVersion = result.Version
	response.Evidence.Visibility.ReadAt = timePtr(result.ReadAt)
	response.Evidence.Visibility.Quality = StageQualityKnown
	if result.Epoch != query.DataEpoch {
		response.Evidence.Visibility.Quality = StageQualityStaleEpoch
		response.Evidence.Visibility.Reason = "index_epoch_mismatch"
	}
	_ = persistent
}

func (service *QueryService) readLogs(ctx context.Context, query StageQuery, persistent persistentStage, response *StageResponse, now time.Time) {
	if service.logs == nil {
		if hasSource(query.LogSources, StageSourceLogs) {
			response.Evidence.Logs = source(StageSourceLogs, now)
			response.Evidence.Logs.Quality = StageQualityBackendError
			response.Evidence.Logs.Reason = "logs_unavailable"
		}
		if hasSource(query.LogSources, StageSourceTrace) {
			response.Evidence.Trace = source(StageSourceTrace, now)
			response.Evidence.Trace.Quality = StageQualityNotConfigured
			response.Evidence.Trace.Sampling = "trace_query_not_configured"
		}
		return
	}
	from, to := query.LogFrom, query.LogTo
	if from.IsZero() {
		to = now
		from = to.Add(-15 * time.Minute)
	}
	correlationQuery := LogCorrelationQuery{DataEpoch: query.DataEpoch, From: from, To: to, Limit: StageMaximumLogs}
	// The log backend accepts exactly one durable selector. Preserve the
	// selector chosen by the caller instead of forwarding every identity that
	// was resolved from the command row.
	switch {
	case query.CommandID != "":
		correlationQuery.CommandID = query.CommandID
	case query.EventID != "":
		correlationQuery.EventID = query.EventID
	default:
		correlationQuery.PostID = query.PostID
	}
	correlation, err := service.logs.QueryCorrelation(ctx, correlationQuery)
	if err != nil {
		if hasSource(query.LogSources, StageSourceLogs) {
			response.Evidence.Logs = source(StageSourceLogs, now)
			response.Evidence.Logs.Quality = StageQualityBackendError
			response.Evidence.Logs.Reason = "logs_unavailable"
		}
		if hasSource(query.LogSources, StageSourceTrace) {
			response.Evidence.Trace = source(StageSourceTrace, now)
			response.Evidence.Trace.Quality = StageQualityBackendError
			response.Evidence.Trace.Reason = "trace_evidence_unavailable"
		}
		return
	}
	if hasSource(query.LogSources, StageSourceLogs) {
		response.Evidence.Logs = source(StageSourceLogs, now)
		response.Evidence.Logs.Quality = correlation.Quality
		response.Evidence.Logs.Truncated = correlation.Truncated
		for _, entry := range correlation.Entries {
			response.Evidence.Logs.Entries = append(response.Evidence.Logs.Entries, stageLogEntry(entry))
		}
		if len(response.Evidence.Logs.Entries) == 0 && response.Evidence.Logs.Quality == "" {
			response.Evidence.Logs.Quality = StageQualityNoEvidence
		}
	}
	if hasSource(query.LogSources, StageSourceTrace) {
		response.Evidence.Trace = source(StageSourceTrace, now)
		for _, entry := range correlation.Entries {
			if entry.TraceID != "" && !contains(response.Evidence.Trace.TraceIDs, entry.TraceID) {
				response.Evidence.Trace.TraceIDs = append(response.Evidence.Trace.TraceIDs, entry.TraceID)
			}
		}
		if len(response.Evidence.Trace.TraceIDs) > 0 {
			response.Evidence.Trace.Quality = StageQualityKnown
		} else if !service.traceConfigured {
			response.Evidence.Trace.Quality = StageQualityNotConfigured
			response.Evidence.Trace.Sampling = "trace_query_not_configured"
		} else {
			response.Evidence.Trace.Quality = StageQualitySampling
			response.Evidence.Trace.Sampling = "trace_id_not_observed"
		}
	}
	_ = persistent
}

func source(name string, now time.Time) StageSource {
	return StageSource{Source: name, ReadAt: timePtr(now)}
}
func timePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	value = value.UTC()
	return &value
}
func hasSource(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func epochQuality(requested, current string) string {
	if requested != current {
		return StageQualityStaleEpoch
	}
	return StageQualityKnown
}
func absentQuality(requested, current string) string {
	if requested != current {
		return StageQualityLegacyUnknown
	}
	return StageQualityNoEvidence
}
func commandEvidence(command Command, current string, now time.Time) StageSource {
	quality := epochQuality(command.DataEpoch, current)
	if !command.RetainedUntil.IsZero() && !command.RetainedUntil.After(now) {
		quality = StageQualityExpired
	}
	return StageSource{Source: StageSourceCommand, Quality: quality, ReadAt: timePtr(now), DataEpoch: command.DataEpoch, CommandID: command.CommandID, EventID: command.EventID, Status: command.Status}
}
func brokerEvidence(row brokerRow, requested, current string, now time.Time) StageSource {
	quality := epochQuality(requested, current)
	if row.DataEpoch == "" {
		quality = StageQualityLegacyUnknown
	}
	result := StageSource{Source: StageSourceBroker, Quality: quality, ReadAt: timePtr(now), DataEpoch: row.DataEpoch, EventID: row.EventID, Status: row.Status, Reason: row.LastError}
	if row.Status == "published" {
		result.Result = "published"
	}
	return result
}
func stageLogEntry(entry LogCorrelationEntry) StageLogEntry {
	return StageLogEntry(entry)
}
func safeStageReason(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 128 {
		return value[:128]
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return ""
		}
	}
	return value
}

// HTTPClient is the platform-side adapter.  It chooses one configured
// private origin, forwards only the session cookie and dedicated credential,
// and performs no retry or hedge.
type HTTPClient struct {
	endpoints  []*url.URL
	credential string
	client     *http.Client
	slots      chan struct{}
	next       atomic.Uint32
}

func NewHTTPClient(endpoints []string, credential string, client *http.Client) (*HTTPClient, error) {
	if len(endpoints) > 2 || len(endpoints) == 0 || len(credential) < 32 {
		return nil, fmt.Errorf("%w: private business endpoint configuration is invalid", ErrInvalidArgument)
	}
	parsed := make([]*url.URL, 0, len(endpoints))
	for _, raw := range endpoints {
		value, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || (value.Scheme != "http" && value.Scheme != "https") || value.Host == "" || value.User != nil || value.RawQuery != "" || value.Fragment != "" || value.Path != "" && value.Path != "/" {
			return nil, fmt.Errorf("%w: private business endpoint is invalid", ErrInvalidArgument)
		}
		value.Path = ""
		parsed = append(parsed, value)
	}
	if client == nil {
		client = &http.Client{}
	}
	// A trusted origin must not be able to turn the platform adapter into a
	// redirecting proxy. Keep the caller's transport, jar, and timeout while
	// making every stage request a single-origin attempt.
	clientCopy := *client
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &HTTPClient{endpoints: parsed, credential: credential, client: &clientCopy, slots: make(chan struct{}, 4)}, nil
}

func (client *HTTPClient) Query(ctx context.Context, query StageQuery) (StageResponse, error) {
	return client.QueryWithSession(ctx, query, "")
}

func (client *HTTPClient) QueryWithSession(ctx context.Context, query StageQuery, cookie string) (StageResponse, error) {
	if client == nil || ctx == nil {
		return StageResponse{}, ErrStageRemote
	}
	if err := query.Validate(); err != nil {
		return StageResponse{}, err
	}
	select {
	case client.slots <- struct{}{}:
		defer func() { <-client.slots }()
	default:
		return StageResponse{}, ErrStageBusy
	}
	requestContext, cancel := context.WithTimeout(ctx, StageBusinessTimeout)
	defer cancel()
	endpoint := client.endpoints[int(client.next.Add(1)-1)%len(client.endpoints)]
	values := url.Values{"epoch": {query.DataEpoch}}
	if query.CommandID != "" {
		values.Set("command_id", query.CommandID)
	}
	if query.EventID != "" {
		values.Set("event_id", query.EventID)
	}
	if query.PostID != 0 {
		values.Set("post_id", strconv.FormatUint(query.PostID, 10))
	}
	if query.CheckVisibility {
		values.Set("check_visibility", "true")
	}
	if len(query.LogSources) > 0 {
		from, to := query.LogFrom, query.LogTo
		if from.IsZero() {
			to = time.Now().UTC()
			from = to.Add(-15 * time.Minute)
		}
		values.Set("include", strings.Join(query.LogSources, ","))
		values.Set("from", from.UTC().Format(time.RFC3339Nano))
		values.Set("to", to.UTC().Format(time.RFC3339Nano))
	}
	address := *endpoint
	address.Path = "/internal/v1/business-stages"
	address.RawQuery = values.Encode()
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, address.String(), nil)
	if err != nil {
		return StageResponse{}, ErrStageRemote
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-GoPulse-Business-Stage-Credential", client.credential)
	if cookie != "" {
		request.Header.Set("Cookie", cookie)
	}
	response, err := client.client.Do(request)
	if err != nil {
		return StageResponse{}, ErrStageRemote
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return StageResponse{}, ErrStageUnauthorized
	}
	if response.StatusCode == http.StatusServiceUnavailable {
		return StageResponse{}, ErrStageBusy
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return StageResponse{}, ErrStageRemote
	}
	var envelope struct {
		Data StageResponse `json:"data"`
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (256<<10)+1))
	if err != nil {
		return StageResponse{}, ErrStageRemote
	}
	if len(body) > 256<<10 {
		return StageResponse{}, ErrStageResponseSize
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return StageResponse{}, ErrStageRemote
	}
	return envelope.Data, nil
}
