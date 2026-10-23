package post

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/businessstate"
	"github.com/Ray-ymq/GoPulse/backend/internal/instrumentation"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/tracing"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var ErrNotFound = errors.New("post not found")

type Repository interface {
	Create(context.Context, uint64, string, string) (Post, error)
	List(context.Context, uint64, ListOptions) ([]Post, error)
	FindPublicByID(context.Context, uint64) (PublicProjection, error)
	LikedByViewer(context.Context, uint64, uint64) (bool, error)
	Exists(context.Context, uint64) (bool, error)
}

type database interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type transactionStarter interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

type RepositoryOptions struct {
	Outbox        outbox.Writer
	BusinessState *businessstate.Repository
	Clock         func() time.Time
	Logger        *slog.Logger
}

type MySQLRepository struct {
	database database
	outbox   outbox.Writer
	state    *businessstate.Repository
	clock    func() time.Time
	logger   *slog.Logger
}

func NewMySQLRepository(database database, options ...RepositoryOptions) *MySQLRepository {
	repository := &MySQLRepository{database: database, clock: time.Now, logger: logging.Discard("backend")}
	if len(options) > 0 {
		repository.outbox = options[0].Outbox
		repository.state = options[0].BusinessState
		if options[0].Logger != nil {
			repository.logger = logging.Module(options[0].Logger, "post")
		}
		if options[0].Clock != nil {
			repository.clock = options[0].Clock
		}
	}
	return repository
}

func NewMySQLRepositoryWithOutbox(database database, eventOutbox outbox.Writer) *MySQLRepository {
	return NewMySQLRepository(database, RepositoryOptions{Outbox: eventOutbox})
}

const postPublicReadSelect = `
SELECT
    p.id,
    p.title,
    p.content,
    p.created_at,
    p.updated_at,
    p.edited_at,
    p.content_revision,
    u.id,
    u.username,
    u.display_name,
    (SELECT COUNT(*) FROM comments AS c WHERE c.post_id = p.id) AS comment_count,
    (SELECT COUNT(*) FROM post_likes AS likes WHERE likes.post_id = p.id) AS like_count
FROM posts AS p %s
INNER JOIN users AS u ON u.id = p.author_id`

const postListReadSelect = `
SELECT
    p.id,
    p.title,
    p.content,
    p.created_at,
    p.updated_at,
    p.edited_at,
    p.content_revision,
    u.id,
    u.username,
    u.display_name,
    (SELECT COUNT(*) FROM comments AS c WHERE c.post_id = p.id) AS comment_count,
    (SELECT COUNT(*) FROM post_likes AS likes WHERE likes.post_id = p.id) AS like_count,
    EXISTS(
        SELECT 1
        FROM post_likes AS my_like
        WHERE my_like.post_id = p.id AND my_like.user_id = ?
    ) AS liked_by_me,
    EXISTS(SELECT 1 FROM user_follows f WHERE f.follower_id = ? AND f.followed_id = u.id) AS following,
    EXISTS(SELECT 1 FROM post_bookmarks b WHERE b.user_id = ? AND b.post_id = p.id) AS bookmarked_by_me,
    %s AS bookmark_created_at
FROM posts AS p %s
INNER JOIN users AS u ON u.id = p.author_id`

func (repository *MySQLRepository) Create(ctx context.Context, authorID uint64, title, content string) (Post, error) {
	if repository.state != nil && repository.outbox != nil {
		result, err := repository.CreateWithCommand(ctx, authorID, title, content, CommandRequest{}, digestCreateInput(CreateInput{Title: title, Content: content}))
		if err != nil {
			return Post{}, err
		}
		if result.Post == nil {
			return Post{}, errors.New("create post: committed result has no post")
		}
		return *result.Post, nil
	}
	if repository.outbox == nil {
		return createPost(ctx, repository.database, authorID, title, content)
	}
	commitAttemptID := tracing.NewAttemptID()
	commitContext, commitSpan := tracing.Start(ctx, "post.commit", attribute.String("gopulse.operation", "create"), attribute.String("gopulse.attempt_id", commitAttemptID))
	commitContext = tracing.WithAttemptID(commitContext, commitAttemptID)
	defer commitSpan.End()
	ctx = commitContext
	commitStarted := time.Now()
	committed := false
	defer func() {
		result := "failure"
		if committed {
			result = "success"
		}
		if metrics := instrumentation.BackendActive(); metrics != nil {
			metrics.ObserveFreshness("commit", result, time.Since(commitStarted))
		}
		if !committed {
			commitSpan.SetStatus(codes.Error, "commit_failed")
		}
	}()
	starter, ok := repository.database.(transactionStarter)
	if !ok {
		return Post{}, errors.New("create post: database does not support transactions")
	}
	transaction, err := starter.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Post{}, errors.New("begin post transaction")
	}
	defer func() { _ = transaction.Rollback() }()
	record, err := createPost(ctx, transaction, authorID, title, content)
	if err != nil {
		return Post{}, err
	}
	event, err := bus.NewPostCreated(repository.clock().UTC(), authorID, record.ID)
	if err != nil {
		return Post{}, errors.New("create post event")
	}
	event.ContentRevision = record.ContentRevision
	commitSpan.SetAttributes(
		attribute.String("gopulse.event_id", event.EventID),
		attribute.Int64("gopulse.post_id", int64(record.ID)),
		attribute.Int64("gopulse.content_revision", int64(record.ContentRevision)),
	)
	outboxID, err := insertOutbox(ctx, transaction, repository.outbox, event)
	if err != nil {
		return Post{}, errors.New("create post outbox event")
	}
	if outboxID > 0 {
		commitSpan.SetAttributes(attribute.Int64("gopulse.outbox_id", int64(outboxID)))
	}
	if err := transaction.Commit(); err != nil {
		return Post{}, errors.New("commit post transaction")
	}
	committed = true
	logging.WithTrace(repository.logger, commitContext).Info("post committed",
		slog.String("event_id", event.EventID),
		slog.Uint64("post_id", record.ID),
		slog.Uint64("content_revision", record.ContentRevision),
		slog.Uint64("outbox_id", outboxID),
		slog.String("attempt_id", commitAttemptID),
		slog.String("stage", "commit"),
		slog.String("result", "success"),
	)
	return record, nil
}

// CreateWithCommand writes a post fact, command result, v2 post.created event,
// and its search obligation in one MySQL transaction. No Redis, broker, ES, or
// telemetry export is performed while the transaction is open.
func (repository *MySQLRepository) CreateWithCommand(ctx context.Context, authorID uint64, title, content string, request CommandRequest, requestDigest []byte) (CreateResult, error) {
	if repository == nil || repository.database == nil || repository.outbox == nil || repository.state == nil {
		return CreateResult{}, ErrCommandActivation
	}
	if ctx == nil || authorID == 0 || len(requestDigest) != 32 {
		return CreateResult{}, errors.New("create post command: invalid arguments")
	}
	if err := validateCommandRequest(request); err != nil {
		return CreateResult{}, err
	}
	starter, ok := repository.database.(transactionStarter)
	if !ok {
		return CreateResult{}, errors.New("create post command: database does not support transactions")
	}
	transaction, err := starter.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return CreateResult{}, errors.New("begin post command transaction")
	}
	defer func() { _ = transaction.Rollback() }()
	environment, err := repository.state.LockCurrentTx(ctx, transaction)
	if err != nil {
		return CreateResult{}, err
	}
	if environment.Status != businessstate.StatusReady || environment.Mode != businessstate.ModeV2 {
		return CreateResult{}, ErrCommandActivation
	}
	now := repository.clock().UTC().Truncate(time.Microsecond)
	keyHash := []byte(nil)
	if request.IdempotencyKey != "" {
		if request.DataEpoch != environment.DataEpoch {
			return CreateResult{}, ErrDataEpochMismatch
		}
		keyHash = commandKeyHash(request.IdempotencyKey)
		existing, found, lookupErr := repository.state.FindCommandTx(ctx, transaction, environment.DataEpoch, authorID, keyHash, now)
		if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
			return CreateResult{}, lookupErr
		}
		if found {
			if !existing.RetainedUntil.IsZero() && !existing.RetainedUntil.After(now) {
				return CreateResult{}, ErrCommandResultUnknown
			}
			if !bytes.Equal(existing.RequestDigest, requestDigest) {
				return CreateResult{}, ErrIdempotencyConflict
			}
			if existing.Status != "succeeded" {
				return CreateResult{}, ErrCommandOutcomeUnknown
			}
			if err := transaction.Commit(); err != nil {
				return CreateResult{}, ErrCommandOutcomeUnknown
			}
			readContext, cancel := commandReadContext(ctx)
			defer cancel()
			return repository.replayResult(readContext, authorID, existing, true, nil), nil
		}
	}
	commandID, err := bus.NewUUID()
	if err != nil {
		return CreateResult{}, errors.New("generate post command ID")
	}
	record, err := createPost(ctx, transaction, authorID, title, content)
	if err != nil {
		return CreateResult{}, err
	}
	event, err := bus.NewV2PostCreated(now, environment.DataEpoch, commandID, authorID, record.ID, record.ContentRevision)
	if err != nil {
		return CreateResult{}, errors.New("create v2 post event")
	}
	setCommandTrace(&event, ctx)
	payload, err := bus.Encode(event)
	if err != nil {
		return CreateResult{}, errors.New("encode v2 post event")
	}
	reservation := businessstate.CommandReservation{
		Command:       businessstate.Command{DataEpoch: environment.DataEpoch, CommandID: commandID, UserID: authorID, CommandType: businessstate.PostCreateCommandType, IdempotencyKeyHash: keyHash, RequestDigest: requestDigest, PostID: record.ID, EventID: event.EventID, Status: "succeeded"},
		RequestDigest: requestDigest, IdempotencyKeyHash: keyHash,
		Requirement:  businessstate.Requirement{DataEpoch: environment.DataEpoch, EventID: event.EventID, Consumer: businessstate.ConsumerSearch, RoutingContractRevision: businessstate.SearchRoutingContractV1, ApplicableObjectVersion: record.ContentRevision, PayloadBytes: len(payload)},
		PayloadBytes: len(payload), Now: now,
	}
	if err := repository.state.ReservePostCreateTx(ctx, transaction, reservation); err != nil {
		if errors.Is(err, businessstate.ErrCommandAlreadyExists) && request.IdempotencyKey != "" {
			// The database uniqueness constraint is the final race boundary. A
			// competing transaction may win after the initial lookup but before
			// this transaction inserts its command. Roll back the losing post
			// fact, then resolve the committed winner through a fresh connection.
			_ = transaction.Rollback()
			lookupContext, cancel := commandReadContext(ctx)
			defer cancel()
			committed, found, lookupErr := repository.state.FindCommand(lookupContext, environment.DataEpoch, authorID, keyHash, now)
			if lookupErr != nil || !found {
				return CreateResult{}, ErrCommandOutcomeUnknown
			}
			if !committed.RetainedUntil.IsZero() && !committed.RetainedUntil.After(now) {
				return CreateResult{}, ErrCommandResultUnknown
			}
			if !bytes.Equal(committed.RequestDigest, requestDigest) {
				return CreateResult{}, ErrIdempotencyConflict
			}
			if committed.Status != "succeeded" {
				return CreateResult{}, ErrCommandOutcomeUnknown
			}
			return repository.replayResult(lookupContext, authorID, committed, true, nil), nil
		}
		if errors.Is(err, businessstate.ErrBudgetExceeded) {
			return CreateResult{}, ErrBusinessAsyncBacklog
		}
		return CreateResult{}, err
	}
	if _, err := insertOutbox(ctx, transaction, repository.outbox, event); err != nil {
		return CreateResult{}, errors.New("create v2 post outbox event")
	}
	if err := repository.state.AccountPostCreateTx(ctx, transaction, reservation); err != nil {
		if errors.Is(err, businessstate.ErrBudgetExceeded) {
			return CreateResult{}, ErrBusinessAsyncBacklog
		}
		return CreateResult{}, err
	}
	if err := transaction.Commit(); err != nil {
		if request.IdempotencyKey == "" {
			return CreateResult{}, ErrCommandOutcomeUnknown
		}
		lookupContext, cancel := commandReadContext(ctx)
		defer cancel()
		committed, found, lookupErr := repository.state.FindCommand(lookupContext, environment.DataEpoch, authorID, keyHash, now)
		if lookupErr == nil && found && committed.Status == "succeeded" && bytes.Equal(committed.RequestDigest, requestDigest) {
			return repository.replayResult(lookupContext, authorID, committed, true, nil), nil
		}
		return CreateResult{}, ErrCommandOutcomeUnknown
	}
	return CreateResult{Post: &record, Meta: commandMeta(commandID, event.EventID, environment.DataEpoch, record.ID, now, false, "available")}, nil
}

func (repository *MySQLRepository) replayResult(ctx context.Context, userID uint64, command businessstate.Command, replayed bool, executor database) CreateResult {
	var record *Post
	resourceState := "unavailable"
	if command.PostID > 0 {
		// The command row is already committed when a replay reaches this
		// path. Read through the repository so viewer-specific permissions and
		// bookmark/like state are recalculated instead of returning a cached
		// author-only projection. The executor is retained in the signature for
		// the first replay transaction's lock scope; it is not used for a
		// second connection's viewer read.
		value, err := repository.FindByID(ctx, command.PostID, userID)
		if err == nil {
			if hydrator, ok := any(repository).(interface {
				HydrateFollowing(context.Context, []Post, uint64) error
			}); ok {
				records := []Post{value}
				if hydrateErr := hydrator.HydrateFollowing(ctx, records, userID); hydrateErr == nil {
					value = records[0]
				}
			}
			record = &value
			resourceState = "available"
		}
	}
	_ = executor
	return CreateResult{Post: record, Meta: commandMeta(command.CommandID, command.EventID, command.DataEpoch, command.PostID, command.CommittedAt, replayed, resourceState)}
}

func (repository *MySQLRepository) CommandContext(ctx context.Context) (CommandContext, error) {
	if repository == nil || repository.state == nil {
		return CommandContext{}, ErrCommandActivation
	}
	value, err := repository.state.ReadCommandContext(ctx)
	if err != nil {
		return CommandContext{}, err
	}
	return CommandContext{DataEpoch: value.DataEpoch, RetryWindow: int64(value.RetryWindow / time.Second), RetryWindowUntil: value.RetryWindowUntil}, nil
}

func (repository *MySQLRepository) CommandResult(ctx context.Context, userID uint64, request CommandRequest) (CreateResult, error) {
	if repository == nil || repository.state == nil {
		return CreateResult{}, ErrCommandActivation
	}
	if err := validateCommandRequest(request); err != nil {
		return CreateResult{}, err
	}
	readContext, cancel := commandReadContext(ctx)
	defer cancel()
	environment, err := repository.state.Current(readContext)
	if err != nil {
		return CreateResult{}, err
	}
	if environment.Status != businessstate.StatusReady || request.DataEpoch != environment.DataEpoch {
		return CreateResult{}, ErrDataEpochMismatch
	}
	keyHash := commandKeyHash(request.IdempotencyKey)
	command, found, err := repository.state.FindCommand(readContext, environment.DataEpoch, userID, keyHash, repository.clock().UTC())
	if err != nil {
		return CreateResult{}, ErrCommandOutcomeUnknown
	}
	if !found || command.Status != "succeeded" {
		return CreateResult{}, ErrCommandResultUnknown
	}
	return repository.replayResult(readContext, userID, command, true, nil), nil
}

// mutationEpoch reads the activation boundary on the fact transaction.
func (repository *MySQLRepository) mutationEpoch(ctx context.Context, tx *sql.Tx) (string, error) {
	if repository.state == nil {
		return "", nil
	}
	environment, err := repository.state.LockCurrentTx(ctx, tx)
	if err != nil {
		return "", err
	}
	if environment.Status != businessstate.StatusReady || environment.Mode != businessstate.ModeV2 {
		return "", businessstate.ErrNotReady
	}
	return environment.DataEpoch, nil
}

func (repository *MySQLRepository) insertSearchMutation(ctx context.Context, tx *sql.Tx, event bus.Envelope, epoch string) (uint64, error) {
	var requirement businessstate.Requirement
	if epoch != "" {
		var err error
		event, err = event.WithDataEpoch(epoch)
		if err != nil {
			return 0, err
		}
		setCommandTrace(&event, ctx)
		payload, err := bus.Encode(event)
		if err != nil {
			return 0, err
		}
		requirement = businessstate.Requirement{
			DataEpoch: epoch, EventID: event.EventID, Consumer: businessstate.ConsumerSearch,
			RoutingContractRevision: businessstate.SearchRoutingContractV1,
			ApplicableObjectVersion: event.ContentRevision, PayloadBytes: len(payload),
		}
	}
	outboxID, err := insertOutbox(ctx, tx, repository.outbox, event)
	if err != nil {
		return 0, err
	}
	// 先写 Outbox 和需求，再取得预算行锁，使后续持锁区间只覆盖预算检查、扣账和提交。
	if epoch != "" {
		if err := repository.state.RegisterRequirementTx(ctx, tx, requirement); err != nil {
			return 0, err
		}
	}
	return outboxID, nil
}

func insertOutbox(ctx context.Context, executor outbox.Executor, writer outbox.Writer, envelope bus.Envelope) (uint64, error) {
	if metadataWriter, ok := writer.(outbox.MetadataWriter); ok {
		return metadataWriter.InsertWithMetadata(ctx, executor, envelope)
	}
	return 0, writer.Insert(ctx, executor, envelope)
}

func createPost(ctx context.Context, database database, authorID uint64, title, content string) (Post, error) {
	result, err := database.ExecContext(ctx, `INSERT INTO posts (author_id, title, content) VALUES (?, ?, ?)`, authorID, title, content)
	if err != nil {
		return Post{}, fmt.Errorf("create post: %w", err)
	}
	identifier, err := result.LastInsertId()
	if err != nil || identifier <= 0 {
		return Post{}, errors.New("create post: invalid inserted identifier")
	}
	projection, err := findPublicByID(ctx, database, uint64(identifier))
	if err != nil {
		return Post{}, err
	}
	return projection.post(false), nil
}

func (repository *MySQLRepository) List(ctx context.Context, viewerID uint64, options ListOptions) ([]Post, error) {
	query, arguments := listStatement(viewerID, options)
	rows, err := repository.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list posts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	posts := make([]Post, 0, options.Limit+1)
	for rows.Next() {
		record, err := scanPost(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("list posts: %w", err)
		}
		posts = append(posts, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list posts: %w", err)
	}
	return posts, nil
}

func listStatement(viewerID uint64, options ListOptions) (string, []any) {
	timestamp := "p.created_at"
	join := ""
	if !options.Bookmarks && !options.Following && options.AuthorID == 0 {
		join = "FORCE INDEX (idx_posts_created_at_id)"
	}
	arguments := []any{viewerID, viewerID, viewerID}
	if options.Bookmarks {
		timestamp = "b.created_at"
		join = "INNER JOIN post_bookmarks b ON b.post_id = p.id AND b.user_id = ?"
		arguments = append(arguments, viewerID)
	}
	if options.Following {
		join += " INNER JOIN (SELECT followed_id FROM user_follows WHERE follower_id=?) AS feed_authors ON feed_authors.followed_id=p.author_id"
		arguments = append(arguments, viewerID)
	}
	filter := " WHERE 1=1"
	if options.AuthorID != 0 {
		filter += " AND p.author_id = ?"
		arguments = append(arguments, options.AuthorID)
	}
	if options.Cursor != nil {
		filter += fmt.Sprintf(" AND (%s < ? OR (%s = ? AND p.id < ?))", timestamp, timestamp)
		arguments = append(arguments, options.Cursor.CreatedAt, options.Cursor.CreatedAt, options.Cursor.ID)
	}
	order := " ORDER BY " + timestamp + " DESC, p.id DESC"
	arguments = append(arguments, options.Limit+1)
	if options.Following && !options.Bookmarks {
		page := "SELECT p.id FROM posts AS p " + join + filter + order + " LIMIT ?"
		query := fmt.Sprintf(postListReadSelect, timestamp, "INNER JOIN ("+page+") AS feed_page ON feed_page.id=p.id") + order
		return query, arguments
	}
	query := fmt.Sprintf(postListReadSelect, timestamp, join) + filter + order + " LIMIT ?"
	return query, arguments
}

func (repository *MySQLRepository) FindPublicByID(ctx context.Context, postID uint64) (PublicProjection, error) {
	return findPublicByID(ctx, repository.database, postID)
}

// ReadContext obtains the epoch, visibility, owner and content revision that
// define one immutable body lookup. The production assembly supplies the
// business-state repository so the epoch read uses the same authoritative
// database connection as the post fact.
func (repository *MySQLRepository) ReadContext(ctx context.Context, postID uint64) (readContext ReadContext, err error) {
	started := time.Now()
	defer func() { observePostDB("post_context", err, time.Since(started)) }()
	if repository == nil || repository.database == nil || postID == 0 {
		return ReadContext{}, errors.New("read post context: invalid argument")
	}
	var epoch, status string
	var authorID, revision sql.NullInt64
	err = repository.database.QueryRowContext(ctx, `
SELECT e.data_epoch, e.status, p.author_id, p.content_revision
FROM business_environment AS e
LEFT JOIN posts AS p ON p.id=?
WHERE e.singleton=1`, postID).Scan(&epoch, &status, &authorID, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return ReadContext{}, errors.New("business environment is not ready")
	}
	if err != nil {
		return ReadContext{}, fmt.Errorf("read post context: %w", err)
	}
	if status != businessstate.StatusReady {
		return ReadContext{}, ErrReadContextChanged
	}
	if businessstate.ValidateEpoch(epoch) != nil {
		return ReadContext{}, errors.New("business environment epoch is invalid")
	}
	if !authorID.Valid || !revision.Valid || authorID.Int64 <= 0 || revision.Int64 <= 0 {
		return ReadContext{}, ErrNotFound
	}
	readContext = ReadContext{Epoch: epoch, PostID: postID, AuthorID: uint64(authorID.Int64), ContentRevision: uint64(revision.Int64), Visible: true, ReadAt: repository.clock().UTC()}
	return readContext, nil
}

// FindBody is version constrained. A revision mismatch is not silently
// rebound to the current row; the caller must start a new request read.
func (repository *MySQLRepository) FindBody(ctx context.Context, key BodyKey) (body Body, err error) {
	started := time.Now()
	defer func() { observePostDB("post_body", err, time.Since(started)) }()
	if repository == nil || repository.database == nil || !key.Valid() {
		return Body{}, errors.New("find post body: invalid argument")
	}
	var epoch, status string
	var id, author, revision sql.NullInt64
	var title, content sql.NullString
	var created, updated, edited sql.NullTime
	err = repository.database.QueryRowContext(ctx, `
SELECT e.data_epoch, e.status, p.id, p.author_id, p.title, p.content,
       p.created_at, p.updated_at, p.edited_at, p.content_revision
FROM business_environment AS e
LEFT JOIN posts AS p ON p.id=? AND p.content_revision=?
WHERE e.singleton=1`, key.PostID, key.ContentRevision).Scan(
		&epoch, &status, &id, &author, &title, &content, &created, &updated, &edited, &revision,
	)
	if err != nil {
		return Body{}, fmt.Errorf("find post body: %w", err)
	}
	if epoch != key.Epoch || status != businessstate.StatusReady {
		return Body{}, ErrReadContextChanged
	}
	if !id.Valid {
		var currentRevision uint64
		checkErr := repository.database.QueryRowContext(ctx, `SELECT content_revision FROM posts WHERE id=?`, key.PostID).Scan(&currentRevision)
		if errors.Is(checkErr, sql.ErrNoRows) {
			return Body{}, ErrNotFound
		}
		if checkErr != nil {
			return Body{}, fmt.Errorf("find post body revision: %w", checkErr)
		}
		return Body{}, ErrReadContextChanged
	}
	body = Body{ID: uint64(id.Int64), AuthorID: uint64(author.Int64), Title: title.String, Content: content.String,
		CreatedAt: created.Time, UpdatedAt: updated.Time, ContentRevision: uint64(revision.Int64)}
	if edited.Valid {
		body.EditedAt = &edited.Time
	}
	if !body.ValidForKey(key) {
		return Body{}, ErrReadContextChanged
	}
	return body, nil
}

// FindDynamic keeps mutable author data and interaction counts on the
// authoritative path. Both counts are read by one SQL statement so a detail
// response never turns a failed count into a fabricated zero.
func (repository *MySQLRepository) FindDynamic(ctx context.Context, postID, authorID uint64) (dynamic DynamicProjection, err error) {
	started := time.Now()
	defer func() { observePostDB("post_dynamic", err, time.Since(started)) }()
	if repository == nil || repository.database == nil || postID == 0 || authorID == 0 {
		return DynamicProjection{}, errors.New("find post dynamic data: invalid argument")
	}
	err = repository.database.QueryRowContext(ctx, `
SELECT u.id, u.username, u.display_name,
       (SELECT COUNT(*) FROM comments AS c WHERE c.post_id=p.id),
       (SELECT COUNT(*) FROM post_likes AS likes WHERE likes.post_id=p.id)
FROM posts AS p INNER JOIN users AS u ON u.id=p.author_id
WHERE p.id=? AND p.author_id=?`, postID, authorID).Scan(
		&dynamic.Author.ID, &dynamic.Author.Username, &dynamic.Author.DisplayName, &dynamic.CommentCount, &dynamic.LikeCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return DynamicProjection{}, ErrNotFound
	}
	if err != nil {
		return DynamicProjection{}, fmt.Errorf("find post dynamic data: %w", err)
	}
	return dynamic, nil
}

func observePostDB(operation string, err error, elapsed time.Duration) {
	if metrics := instrumentation.BackendActive(); metrics != nil {
		result := "success"
		if errors.Is(err, ErrReadContextChanged) {
			result = "version_changed"
		} else if err != nil {
			result = "failure"
		}
		metrics.ObserveDBOperation(operation, result, elapsed)
	}
}

func findPublicByID(ctx context.Context, database database, postID uint64) (PublicProjection, error) {
	row := database.QueryRowContext(ctx, fmt.Sprintf(postPublicReadSelect, "")+`
WHERE p.id = ?`, postID)
	projection, err := scanPublicProjection(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return PublicProjection{}, ErrNotFound
	}
	if err != nil {
		return PublicProjection{}, fmt.Errorf("find public post detail: %w", err)
	}
	return projection, nil
}

func (repository *MySQLRepository) LikedByViewer(ctx context.Context, postID, viewerID uint64) (liked bool, err error) {
	started := time.Now()
	defer func() { observePostDB("post_viewer", err, time.Since(started)) }()
	if viewerID == 0 {
		return false, nil
	}
	if err = repository.database.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM post_likes WHERE post_id = ? AND user_id = ?)`,
		postID,
		viewerID,
	).Scan(&liked); err != nil {
		return false, fmt.Errorf("check viewer post like: %w", err)
	}
	return liked, nil
}

// FindByID remains as a repository convenience for direct integration checks;
// application detail reads use the explicitly separated public and viewer queries.
func (repository *MySQLRepository) FindByID(ctx context.Context, postID, viewerID uint64) (Post, error) {
	projection, err := repository.FindPublicByID(ctx, postID)
	if err != nil {
		return Post{}, err
	}
	liked, err := repository.LikedByViewer(ctx, postID, viewerID)
	if err != nil {
		return Post{}, err
	}
	records := []Post{projection.post(liked)}
	if err := repository.HydrateBookmarks(ctx, records, viewerID); err != nil {
		return Post{}, err
	}
	return records[0], nil
}

func (repository *MySQLRepository) Exists(ctx context.Context, postID uint64) (bool, error) {
	var exists bool
	if err := repository.database.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM posts WHERE id = ?)`,
		postID,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check post existence: %w", err)
	}
	return exists, nil
}

type scanFunc func(...any) error

func scanPublicProjection(scan scanFunc) (PublicProjection, error) {
	var projection PublicProjection
	err := scan(
		&projection.ID,
		&projection.Title,
		&projection.Content,
		&projection.CreatedAt,
		&projection.UpdatedAt,
		&projection.EditedAt,
		&projection.ContentRevision,
		&projection.Author.ID,
		&projection.Author.Username,
		&projection.Author.DisplayName,
		&projection.CommentCount,
		&projection.LikeCount,
	)
	return projection, err
}

func scanPost(scan scanFunc) (Post, error) {
	var record Post
	err := scan(
		&record.ID,
		&record.Title,
		&record.Content,
		&record.CreatedAt,
		&record.UpdatedAt,
		&record.EditedAt,
		&record.ContentRevision,
		&record.Author.ID,
		&record.Author.Username,
		&record.Author.DisplayName,
		&record.CommentCount,
		&record.LikeCount,
		&record.LikedByMe,
		&record.Author.Following,
		&record.BookmarkedByMe,
		&record.BookmarkCreatedAt,
	)
	return record, err
}

// RowQueryer is the small query boundary required by transactional fact
// writers. It deliberately accepts both *sql.DB and *sql.Tx without exposing
// transaction handles to HTTP handlers.
type RowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// FindAuthorIDForUpdate reads and locks the post author inside the caller's
// transaction. The author is the recipient for comment and like events.
func FindAuthorIDForUpdate(ctx context.Context, database RowQueryer, postID uint64) (uint64, error) {
	if database == nil || postID == 0 {
		return 0, errors.New("find post author: invalid argument")
	}
	var authorID uint64
	if err := database.QueryRowContext(ctx,
		`SELECT author_id FROM posts WHERE id = ? FOR UPDATE`,
		postID,
	).Scan(&authorID); errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	} else if err != nil {
		return 0, fmt.Errorf("find post author: %w", err)
	}
	return authorID, nil
}

// FindMany hydrates one bounded ordered set of search hits from MySQL. The SQL
// query is set based; the requested Elasticsearch order is restored in Go.
func (repository *MySQLRepository) FindMany(ctx context.Context, viewerID uint64, identifiers []uint64) ([]Post, error) {
	if viewerID == 0 || len(identifiers) == 0 || len(identifiers) > MaximumLimit {
		return nil, errors.New("find many posts: invalid arguments")
	}
	placeholders := make([]string, len(identifiers))
	arguments := make([]any, 0, len(identifiers)+2)
	arguments = append(arguments, viewerID, viewerID, viewerID)
	for index, identifier := range identifiers {
		if identifier == 0 {
			return nil, errors.New("find many posts: invalid identifier")
		}
		placeholders[index] = "?"
		arguments = append(arguments, identifier)
	}
	query := fmt.Sprintf(postListReadSelect, "p.created_at", "") + `
WHERE p.id IN (` + strings.Join(placeholders, ",") + `)`
	rows, err := repository.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("find many posts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	byID := make(map[uint64]Post, len(identifiers))
	for rows.Next() {
		record, err := scanPost(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("find many posts: %w", err)
		}
		byID[record.ID] = record
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("find many posts: %w", err)
	}
	ordered := make([]Post, 0, len(identifiers))
	for _, identifier := range identifiers {
		record, ok := byID[identifier]
		if !ok {
			continue
		}
		ordered = append(ordered, record)
	}
	return ordered, nil
}

// HydrateAuthors refreshes mutable summaries after cache reads, in one query.
func (repository *MySQLRepository) HydrateAuthors(ctx context.Context, records []Post) (err error) {
	started := time.Now()
	defer func() { observePostDB("post_viewer", err, time.Since(started)) }()
	if len(records) == 0 {
		return nil
	}
	ids := make([]any, 0, len(records))
	slots := make([]string, 0, len(records))
	for _, p := range records {
		ids = append(ids, p.Author.ID)
		slots = append(slots, "?")
	}
	rows, err := repository.database.QueryContext(ctx, "SELECT id, username, display_name FROM users WHERE id IN ("+strings.Join(slots, ",")+")", ids...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	authors := map[uint64]Author{}
	for rows.Next() {
		var a Author
		if err = rows.Scan(&a.ID, &a.Username, &a.DisplayName); err != nil {
			return err
		}
		authors[a.ID] = a
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for i := range records {
		a, ok := authors[records[i].Author.ID]
		if !ok {
			return ErrNotFound
		}
		records[i].Author = a
	}
	return nil
}

func (r *MySQLRepository) HydrateFollowing(ctx context.Context, records []Post, viewer uint64) (err error) {
	started := time.Now()
	defer func() { observePostDB("post_viewer", err, time.Since(started)) }()
	if len(records) == 0 || viewer == 0 {
		return nil
	}
	args := []any{viewer}
	slots := []string{}
	for _, p := range records {
		args = append(args, p.Author.ID)
		slots = append(slots, "?")
	}
	rows, err := r.database.QueryContext(ctx, "SELECT followed_id FROM user_follows WHERE follower_id = ? AND followed_id IN ("+strings.Join(slots, ",")+")", args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	followed := map[uint64]bool{}
	for rows.Next() {
		var id uint64
		if err = rows.Scan(&id); err != nil {
			return err
		}
		followed[id] = true
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for i := range records {
		records[i].Author.Following = followed[records[i].Author.ID]
	}
	return nil
}

// HydrateBookmarks never writes personalized facts into a shared projection.
func (r *MySQLRepository) HydrateBookmarks(ctx context.Context, records []Post, viewer uint64) (err error) {
	started := time.Now()
	defer func() { observePostDB("post_viewer", err, time.Since(started)) }()
	for i := range records {
		records[i].BookmarkedByMe = false
	}
	if viewer == 0 || len(records) == 0 {
		return nil
	}
	args := []any{viewer}
	slots := []string{}
	for _, p := range records {
		args = append(args, p.ID)
		slots = append(slots, "?")
	}
	rows, err := r.database.QueryContext(ctx, "SELECT post_id FROM post_bookmarks WHERE user_id = ? AND post_id IN ("+strings.Join(slots, ",")+")", args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	found := map[uint64]bool{}
	for rows.Next() {
		var id uint64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		found[id] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range records {
		records[i].BookmarkedByMe = found[records[i].ID]
	}
	return nil
}

// FindViewerProjection rechecks the authoritative key in the same SQL snapshot
// as counts, author data and viewer relationships.
func (repository *MySQLRepository) FindViewerProjection(ctx context.Context, expected ReadContext, viewer uint64) (projection ViewerProjection, err error) {
	started := time.Now()
	defer func() { observePostDB("post_dynamic", err, time.Since(started)) }()
	var epoch, status string
	var author, revision uint64
	err = repository.database.QueryRowContext(ctx, `
 SELECT e.data_epoch,e.status,p.author_id,p.content_revision,u.id,u.username,u.display_name,
 (SELECT COUNT(*) FROM comments c WHERE c.post_id=p.id),
 (SELECT COUNT(*) FROM post_likes l WHERE l.post_id=p.id),
 EXISTS(SELECT 1 FROM post_likes l WHERE l.post_id=p.id AND l.user_id=?),
 EXISTS(SELECT 1 FROM user_follows f WHERE f.followed_id=p.author_id AND f.follower_id=?),
 EXISTS(SELECT 1 FROM post_bookmarks b WHERE b.post_id=p.id AND b.user_id=?)
 FROM business_environment e JOIN posts p ON p.id=? JOIN users u ON u.id=p.author_id WHERE e.singleton=1`,
		viewer, viewer, viewer, expected.PostID).Scan(&epoch, &status, &author, &revision, &projection.Author.ID, &projection.Author.Username, &projection.Author.DisplayName, &projection.CommentCount, &projection.LikeCount, &projection.Liked, &projection.Following, &projection.Bookmarked)
	if errors.Is(err, sql.ErrNoRows) {
		return ViewerProjection{}, ErrNotFound
	}
	if err != nil {
		return ViewerProjection{}, fmt.Errorf("find viewer projection: %w", err)
	}
	if epoch != expected.Epoch || status != businessstate.StatusReady || author != expected.AuthorID || revision != expected.ContentRevision {
		return ViewerProjection{}, ErrReadContextChanged
	}
	return projection, nil
}
