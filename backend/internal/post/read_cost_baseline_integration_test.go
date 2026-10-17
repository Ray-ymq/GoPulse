//go:build integration

package post_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	rediscache "github.com/Ray-ymq/GoPulse/backend/internal/platform/redis"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
)

// TestIntegrationPostDetailReadCostBaseline records the Phase 1 read-cost
// evidence for the legacy detail path. The test intentionally logs evidence
// instead of asserting a performance threshold; thresholds are frozen only
// after this baseline has been reviewed.
func TestIntegrationPostDetailReadCostBaseline(t *testing.T) {
	cfg := integrationtest.Environment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("OpenMySQLDatabase() error = %v", err)
	}
	defer database.Close()
	releasePostFactsLock := integrationtest.AcquirePostFactsLock(t, database)
	defer releasePostFactsLock()

	authorID, viewerID, postID := createReadCostFixture(t, ctx, database)
	defer cleanupReadCostFixture(t, database, authorID, viewerID, postID)
	if _, err := database.ExecContext(ctx, `INSERT INTO user_follows (follower_id, followed_id) VALUES (?, ?)`, viewerID, authorID); err != nil {
		t.Fatalf("insert baseline follow: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO post_bookmarks (user_id, post_id) VALUES (?, ?)`, viewerID, postID); err != nil {
		t.Fatalf("insert baseline bookmark: %v", err)
	}

	epoch, err := readCostEpoch(ctx, database)
	if err != nil {
		t.Fatalf("read current data epoch: %v", err)
	}
	redisClient := platform.NewRedis(cfg.Redis)
	defer redisClient.Close()
	if err := redisClient.Check(ctx); err != nil {
		t.Fatalf("Redis Check() error = %v", err)
	}
	_ = redisClient.Delete(ctx, rediscache.PostDetailKeyForEpoch(epoch, postID))

	legacy, closeLegacy, err := newSerialReadCostService(ctx, database, cfg, redisClient, epoch, false, postID, viewerID)
	if err != nil {
		t.Fatalf("open serial baseline connection: %v", err)
	}
	measureReadCost(t, ctx, "cold_cache", legacy, false)
	closeLegacy()

	legacy, closeLegacy, err = newSerialReadCostService(ctx, database, cfg, redisClient, epoch, false, postID, viewerID)
	if err != nil {
		t.Fatalf("open serial warm connection: %v", err)
	}
	measureReadCost(t, ctx, "warm_hit", legacy, false)
	closeLegacy()

	if _, err := database.ExecContext(ctx, `INSERT INTO comments (post_id, author_id, content) VALUES (?, ?, ?)`, postID, viewerID, "baseline interaction"); err != nil {
		t.Fatalf("insert baseline comment: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO post_likes (post_id, user_id) VALUES (?, ?)`, postID, viewerID); err != nil {
		t.Fatalf("insert baseline like: %v", err)
	}
	legacy, closeLegacy, err = newSerialReadCostService(ctx, database, cfg, redisClient, epoch, false, postID, viewerID)
	if err != nil {
		t.Fatalf("open serial interaction connection: %v", err)
	}
	measureReadCost(t, ctx, "normal_interaction", legacy, false)
	closeLegacy()

	failedDatabase, err := platform.OpenMySQLDatabase(config.MySQLConfig{
		Host: "127.0.0.1", Port: 1, Database: cfg.MySQL.Database, User: cfg.MySQL.User, Password: cfg.MySQL.Password,
		MaxOpenConns: 1, MaxIdleConns: 0, ConnMaxLifetime: time.Minute,
	})
	if err != nil {
		t.Fatalf("OpenMySQLDatabase(failure fixture) error = %v", err)
	}
	failingRedis := &readCostRedisClient{client: redisClient}
	failingService := newReadCostService(failedDatabase, failedDatabase, cfg, failingRedis, epoch, true, postID, viewerID)
	measureReadCost(t, ctx, "mysql_failure", failingService, true)
	_ = failedDatabase.Close()

	restore, closeRestore, err := newSerialReadCostService(ctx, database, cfg, redisClient, epoch, false, postID, viewerID)
	if err != nil {
		t.Fatalf("open serial cache restore connection: %v", err)
	}
	if _, err := restore.service.Detail(ctx, postID, viewerID); err != nil {
		t.Fatalf("restore cache after MySQL failure: %v", err)
	}
	closeRestore()

	if _, err := database.ExecContext(ctx, `UPDATE posts SET title=?, content=?, content_revision=content_revision+1, updated_at=? WHERE id=?`, "baseline revision 2", "baseline content revision 2", time.Now().UTC(), postID); err != nil {
		t.Fatalf("advance content revision: %v", err)
	}
	versionChanged, closeVersion, err := newSerialReadCostService(ctx, database, cfg, redisClient, epoch, false, postID, viewerID)
	if err != nil {
		t.Fatalf("open serial version connection: %v", err)
	}
	measureReadCost(t, ctx, "content_revision_change", versionChanged, false)
	closeVersion()

	redisFailure, closeRedisFailure, err := newSerialReadCostService(ctx, database, cfg, &readCostRedisClient{client: redisClient, unavailable: true}, epoch, false, postID, viewerID)
	if err != nil {
		t.Fatalf("open serial Redis failure connection: %v", err)
	}
	measureReadCost(t, ctx, "redis_failure", redisFailure, false)
	closeRedisFailure()

	concurrent := newReadCostService(database, database, cfg, redisClient, epoch, false, postID, viewerID)
	measureConcurrentWarm(t, ctx, concurrent, 16)

	logReadCostPlans(t, ctx, database, postID, viewerID, authorID)
}

type readCostSample struct {
	Scenario         string `json:"scenario"`
	Outcome          string `json:"outcome"`
	Error            string `json:"error,omitempty"`
	ElapsedMicros    int64  `json:"elapsed_us"`
	SQLCalls         int    `json:"sql_calls"`
	SQLQueryCalls    int    `json:"sql_query_calls"`
	SQLQueryRowCalls int    `json:"sql_query_row_calls"`
	EpochCalls       int    `json:"epoch_calls"`
	RedisGetCalls    int    `json:"redis_get_calls"`
	RedisSetCalls    int    `json:"redis_set_calls"`
	RedisDeleteCalls int    `json:"redis_delete_calls"`
	PoolWaitCount    int64  `json:"pool_wait_count"`
	PoolWaitMicros   int64  `json:"pool_wait_us"`
	RowsRead         int64  `json:"rows_read"`
	RowsSent         int64  `json:"rows_sent"`
	BytesSent        int64  `json:"bytes_sent"`
	Requests         int    `json:"requests,omitempty"`
}

type readCostDatabase struct {
	database postDatabase

	mu            sync.Mutex
	queryCalls    int
	queryRowCalls int
}

func (counter *readCostDatabase) ExecContext(ctx context.Context, query string, arguments ...any) (sql.Result, error) {
	return counter.database.ExecContext(ctx, query, arguments...)
}

func (counter *readCostDatabase) QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error) {
	counter.mu.Lock()
	counter.queryCalls++
	counter.mu.Unlock()
	return counter.database.QueryContext(ctx, query, arguments...)
}

func (counter *readCostDatabase) QueryRowContext(ctx context.Context, query string, arguments ...any) *sql.Row {
	counter.mu.Lock()
	counter.queryRowCalls++
	counter.mu.Unlock()
	return counter.database.QueryRowContext(ctx, query, arguments...)
}

func (counter *readCostDatabase) reset() {
	counter.mu.Lock()
	counter.queryCalls = 0
	counter.queryRowCalls = 0
	counter.mu.Unlock()
}

func (counter *readCostDatabase) calls() (int, int, int) {
	counter.mu.Lock()
	defer counter.mu.Unlock()
	return counter.queryCalls + counter.queryRowCalls, counter.queryCalls, counter.queryRowCalls
}

type readCostEpochProvider struct {
	database postDatabase
	mu       sync.Mutex
	calls    int
}

func (provider *readCostEpochProvider) CurrentEpoch(ctx context.Context) (string, error) {
	provider.mu.Lock()
	provider.calls++
	provider.mu.Unlock()
	var epoch string
	err := provider.database.QueryRowContext(ctx, `SELECT data_epoch FROM business_environment WHERE singleton=1`).Scan(&epoch)
	return epoch, err
}

func (provider *readCostEpochProvider) reset() {
	provider.mu.Lock()
	provider.calls = 0
	provider.mu.Unlock()
}

func (provider *readCostEpochProvider) count() int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.calls
}

type readCostRedisClient struct {
	client      rediscache.Client
	unavailable bool

	mu      sync.Mutex
	gets    int
	sets    int
	deletes int
}

func (client *readCostRedisClient) Get(ctx context.Context, key string) (string, error) {
	client.mu.Lock()
	client.gets++
	client.mu.Unlock()
	if client.unavailable {
		return "", context.DeadlineExceeded
	}
	return client.client.Get(ctx, key)
}

func (client *readCostRedisClient) Set(ctx context.Context, key, value string, expiration time.Duration) error {
	client.mu.Lock()
	client.sets++
	client.mu.Unlock()
	if client.unavailable {
		return context.DeadlineExceeded
	}
	return client.client.Set(ctx, key, value, expiration)
}

func (client *readCostRedisClient) Delete(ctx context.Context, key string) error {
	client.mu.Lock()
	client.deletes++
	client.mu.Unlock()
	if client.unavailable {
		return context.DeadlineExceeded
	}
	return client.client.Delete(ctx, key)
}

func (client *readCostRedisClient) reset() {
	client.mu.Lock()
	client.gets = 0
	client.sets = 0
	client.deletes = 0
	client.mu.Unlock()
}

func (client *readCostRedisClient) counts() (int, int, int) {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.gets, client.sets, client.deletes
}

type readCostService struct {
	service  *post.Service
	postID   uint64
	viewerID uint64
	counter  *readCostDatabase
	epoch    readCostEpochCounter
	redis    *readCostRedisClient
	statsDB  *sql.DB
	session  *sql.Conn
}

type readCostEpochCounter interface {
	CurrentEpoch(context.Context) (string, error)
	reset()
	count() int
}

type postDatabase interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func newReadCostService(database postDatabase, statsDB *sql.DB, cfg config.Config, redisClient rediscache.Client, epoch string, fixedEpoch bool, postID, viewerID uint64) *readCostService {
	counter := &readCostDatabase{database: database}
	var provider readCostEpochCounter
	if fixedEpoch {
		provider = &fixedReadCostEpochProvider{epoch: epoch}
	} else {
		provider = &readCostEpochProvider{database: counter}
	}
	cache := rediscache.NewPostDetailRepository(redisClient, cfg.Redis.PostDetailTTL, cfg.Redis.OperationTimeout).WithEpochProvider(provider)
	redisCounter, ok := redisClient.(*readCostRedisClient)
	if !ok {
		redisCounter = &readCostRedisClient{client: redisClient}
		cache = rediscache.NewPostDetailRepository(redisCounter, cfg.Redis.PostDetailTTL, cfg.Redis.OperationTimeout).WithEpochProvider(provider)
	}
	service := &readCostService{
		service: post.NewService(post.NewMySQLRepository(counter), cache),
		postID:  postID, viewerID: viewerID, counter: counter, epoch: provider,
		redis: redisCounter, statsDB: statsDB,
	}
	if conn, ok := database.(*sql.Conn); ok {
		service.session = conn
	}
	return service
}

func newSerialReadCostService(ctx context.Context, database *sql.DB, cfg config.Config, redisClient rediscache.Client, epoch string, fixedEpoch bool, postID, viewerID uint64) (*readCostService, func(), error) {
	connection, err := database.Conn(ctx)
	if err != nil {
		return nil, nil, err
	}
	service := newReadCostService(connection, database, cfg, redisClient, epoch, fixedEpoch, postID, viewerID)
	return service, func() { _ = connection.Close() }, nil
}

func (service *readCostService) reset() {
	service.counter.reset()
	service.epoch.reset()
	service.redis.reset()
}

type fixedReadCostEpochProvider struct {
	epoch string
	mu    sync.Mutex
	calls int
}

func (provider *fixedReadCostEpochProvider) CurrentEpoch(context.Context) (string, error) {
	provider.mu.Lock()
	provider.calls++
	provider.mu.Unlock()
	if provider.epoch == "" {
		return "", errors.New("fixed read cost epoch is empty")
	}
	return provider.epoch, nil
}

func (provider *fixedReadCostEpochProvider) reset() {
	provider.mu.Lock()
	provider.calls = 0
	provider.mu.Unlock()
}

func (provider *fixedReadCostEpochProvider) count() int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.calls
}

func measureReadCost(t *testing.T, ctx context.Context, scenario string, service *readCostService, wantError bool) readCostSample {
	t.Helper()
	service.reset()
	beforePool := sql.DBStats{}
	if service.statsDB != nil {
		beforePool = service.statsDB.Stats()
	}
	beforeSession := readCostSessionStats{}
	if service.session != nil {
		beforeSession = readCostSession(ctx, service.session)
	}
	started := time.Now()
	_, err := service.service.Detail(ctx, service.postID, service.viewerID)
	elapsed := time.Since(started)
	afterPool := beforePool
	if service.statsDB != nil {
		afterPool = service.statsDB.Stats()
	}
	afterSession := readCostSessionStats{}
	if service.session != nil {
		afterSession = readCostSession(ctx, service.session)
	}
	sample := buildReadCostSample(scenario, service, elapsed, err, beforePool, afterPool, beforeSession, afterSession, 1)
	if wantError && err == nil {
		t.Fatalf("%s error = nil, want database failure", scenario)
	}
	if !wantError && err != nil {
		t.Fatalf("%s error = %v", scenario, err)
	}
	logReadCostSample(t, sample)
	return sample
}

func measureConcurrentWarm(t *testing.T, ctx context.Context, service *readCostService, requests int) readCostSample {
	t.Helper()
	service.reset()
	beforePool := service.statsDB.Stats()
	started := time.Now()
	errorsCh := make(chan error, requests)
	var waitGroup sync.WaitGroup
	for i := 0; i < requests; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			_, err := service.service.Detail(ctx, service.postID, service.viewerID)
			errorsCh <- err
		}()
	}
	waitGroup.Wait()
	close(errorsCh)
	var firstErr error
	for err := range errorsCh {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	sample := buildReadCostSample("concurrent_warm_hit", service, time.Since(started), firstErr, beforePool, service.statsDB.Stats(), readCostSessionStats{}, readCostSessionStats{}, requests)
	if firstErr != nil {
		t.Fatalf("concurrent_warm_hit error = %v", firstErr)
	}
	logReadCostSample(t, sample)
	return sample
}

func buildReadCostSample(scenario string, service *readCostService, elapsed time.Duration, err error, beforePool, afterPool sql.DBStats, beforeSession, afterSession readCostSessionStats, requests int) readCostSample {
	sqlCalls, queryCalls, queryRowCalls := service.counter.calls()
	redisGets, redisSets, redisDeletes := service.redis.counts()
	sample := readCostSample{
		Scenario: scenario, Outcome: "success", ElapsedMicros: elapsed.Microseconds(),
		SQLCalls: sqlCalls, SQLQueryCalls: queryCalls, SQLQueryRowCalls: queryRowCalls,
		EpochCalls: service.epoch.count(), RedisGetCalls: redisGets, RedisSetCalls: redisSets, RedisDeleteCalls: redisDeletes,
		PoolWaitCount: afterPool.WaitCount - beforePool.WaitCount, PoolWaitMicros: (afterPool.WaitDuration - beforePool.WaitDuration).Microseconds(),
		RowsRead:  afterSession.RowsRead - beforeSession.RowsRead,
		RowsSent:  afterSession.RowsSent - beforeSession.RowsSent,
		BytesSent: afterSession.BytesSent - beforeSession.BytesSent, Requests: requests,
	}
	if err != nil {
		sample.Outcome = "error"
		sample.Error = err.Error()
	}
	return sample
}

func logReadCostSample(t *testing.T, sample readCostSample) {
	t.Helper()
	payload, err := json.Marshal(sample)
	if err != nil {
		t.Fatalf("marshal read cost sample: %v", err)
	}
	t.Logf("READ_COST_BASELINE %s", payload)
}

type readCostSessionStats struct {
	RowsRead  int64
	RowsSent  int64
	BytesSent int64
}

func readCostSession(ctx context.Context, connection *sql.Conn) readCostSessionStats {
	rows, err := connection.QueryContext(ctx, `SHOW SESSION STATUS WHERE Variable_name IN ('Rows_read', 'Rows_sent', 'Bytes_sent')`)
	if err != nil {
		return readCostSessionStats{}
	}
	defer rows.Close()
	result := readCostSessionStats{}
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			return readCostSessionStats{}
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			continue
		}
		switch strings.ToLower(name) {
		case "rows_read":
			result.RowsRead = parsed
		case "rows_sent":
			result.RowsSent = parsed
		case "bytes_sent":
			result.BytesSent = parsed
		}
	}
	return result
}

func createReadCostFixture(t *testing.T, ctx context.Context, database *sql.DB) (uint64, uint64, uint64) {
	t.Helper()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	suffix := strconv.FormatInt(time.Now().UnixNano()%1000000000, 10)
	authorID := insertReadCostUser(t, ctx, tx, "ReadCostA"+suffix)
	viewerID := insertReadCostUser(t, ctx, tx, "ReadCostV"+suffix)
	postID := insertReadCostPost(t, ctx, tx, authorID, "read cost baseline", time.Now().UTC())
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit read cost fixture: %v", err)
	}
	return authorID, viewerID, postID
}

func insertReadCostUser(t *testing.T, ctx context.Context, tx *sql.Tx, username string) uint64 {
	t.Helper()
	result, err := tx.ExecContext(ctx, `INSERT INTO users (username, password_hash) VALUES (?, ?)`, username, "$2a$10$integration-placeholder")
	if err != nil {
		t.Fatalf("insert read cost user %q: %v", username, err)
	}
	identifier, err := result.LastInsertId()
	if err != nil || identifier <= 0 {
		t.Fatalf("insert read cost user %q id=%d error=%v", username, identifier, err)
	}
	return uint64(identifier)
}

func insertReadCostPost(t *testing.T, ctx context.Context, tx *sql.Tx, authorID uint64, title string, createdAt time.Time) uint64 {
	t.Helper()
	result, err := tx.ExecContext(ctx, `INSERT INTO posts (author_id, title, content, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, authorID, title, "content: "+title, createdAt, createdAt)
	if err != nil {
		t.Fatalf("insert read cost post: %v", err)
	}
	identifier, err := result.LastInsertId()
	if err != nil || identifier <= 0 {
		t.Fatalf("insert read cost post id=%d error=%v", identifier, err)
	}
	return uint64(identifier)
}

func cleanupReadCostFixture(t *testing.T, database *sql.DB, authorID, viewerID, postID uint64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	queries := []struct {
		query string
		args  []any
	}{
		{`DELETE FROM post_bookmarks WHERE post_id=? OR user_id=?`, []any{postID, viewerID}},
		{`DELETE FROM user_follows WHERE follower_id=? OR followed_id=?`, []any{viewerID, authorID}},
		{`DELETE FROM post_likes WHERE post_id=? OR user_id=?`, []any{postID, viewerID}},
		{`DELETE FROM comments WHERE post_id=? OR author_id=?`, []any{postID, viewerID}},
		{`DELETE FROM posts WHERE id=?`, []any{postID}},
		{`DELETE FROM users WHERE id IN (?, ?)`, []any{authorID, viewerID}},
	}
	for _, item := range queries {
		if _, err := database.ExecContext(ctx, item.query, item.args...); err != nil {
			t.Errorf("cleanup read cost fixture: %v", err)
		}
	}
}

func readCostEpoch(ctx context.Context, database *sql.DB) (string, error) {
	var epoch string
	err := database.QueryRowContext(ctx, `SELECT data_epoch FROM business_environment WHERE singleton=1`).Scan(&epoch)
	return epoch, err
}

func logReadCostPlans(t *testing.T, ctx context.Context, database *sql.DB, postID, viewerID, authorID uint64) {
	t.Helper()
	type planQuery struct {
		name  string
		query string
		args  []any
	}
	queries := []planQuery{
		{name: "public_detail", query: readCostPublicDetailQuery + ` WHERE p.id=?`, args: []any{postID}},
		{name: "content_revision", query: `SELECT content_revision FROM posts WHERE id=?`, args: []any{postID}},
		{name: "dynamic", query: `SELECT u.id, u.username, u.display_name,
       (SELECT COUNT(*) FROM comments AS c WHERE c.post_id=p.id),
       (SELECT COUNT(*) FROM post_likes AS likes WHERE likes.post_id=p.id)
FROM posts AS p INNER JOIN users AS u ON u.id=p.author_id
WHERE p.id=? AND p.author_id=?`, args: []any{postID, authorID}},
		{name: "viewer_like", query: `SELECT EXISTS(SELECT 1 FROM post_likes WHERE post_id=? AND user_id=?)`, args: []any{postID, viewerID}},
		{name: "following", query: `SELECT followed_id FROM user_follows WHERE follower_id=? AND followed_id IN (?)`, args: []any{viewerID, authorID}},
		{name: "bookmark", query: `SELECT post_id FROM post_bookmarks WHERE user_id=? AND post_id IN (?)`, args: []any{viewerID, postID}},
	}
	for _, item := range queries {
		var plan string
		if err := database.QueryRowContext(ctx, "EXPLAIN FORMAT=JSON "+item.query, item.args...).Scan(&plan); err != nil {
			t.Logf("READ_COST_PLAN name=%s error=%v", item.name, err)
			continue
		}
		t.Logf("READ_COST_PLAN name=%s plan=%s", item.name, strings.Join(strings.Fields(plan), " "))
	}
}

const readCostPublicDetailQuery = `
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
FROM posts AS p
INNER JOIN users AS u ON u.id = p.author_id`
