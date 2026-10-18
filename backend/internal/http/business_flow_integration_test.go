//go:build integration

package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	stdhttp "net/http"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/auth"
	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/businessstate"
	"github.com/Ray-ymq/GoPulse/backend/internal/comment"
	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/like"
	"github.com/Ray-ymq/GoPulse/backend/internal/notification"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	rediscache "github.com/Ray-ymq/GoPulse/backend/internal/platform/redis"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/Ray-ymq/GoPulse/backend/internal/search"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
	"github.com/Ray-ymq/GoPulse/backend/internal/worker"
	"github.com/gin-gonic/gin"
)

func TestIntegrationBusinessFlowUsesOutboxWorkerAndSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := integrationtest.Environment(t)
	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("OpenMySQLDatabase() error = %v", err)
	}
	defer database.Close()
	release := integrationtest.AcquirePostFactsLock(t, database)
	defer release()
	state, err := businessstate.NewRepository(database)
	if err != nil {
		t.Fatalf("business state repository error = %v", err)
	}
	contextForBaseline, cancelBaseline := context.WithTimeout(context.Background(), 10*time.Second)
	environment, err := state.Ensure(contextForBaseline)
	if err != nil {
		cancelBaseline()
		t.Fatalf("business state ensure error = %v", err)
	}
	baselineBudget, err := state.Budget(contextForBaseline, environment.DataEpoch)
	cancelBaseline()
	if err != nil {
		t.Fatalf("business state budget baseline error = %v", err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	if len(suffix) > 10 {
		suffix = suffix[len(suffix)-10:]
	}
	ownerName := "FlowOwner_" + suffix
	actorName := "FlowActor_" + suffix
	defer cleanupHTTPPostUsers(t, cfg, ownerName, actorName)
	defer cleanupBusinessFlowData(t, cfg, environment.DataEpoch, baselineBudget, ownerName, actorName)
	router, stopRuntime, waitRuntime := integrationBusinessRouter(t, cfg, database)
	defer stopRuntime()
	if err := waitRuntime(45 * time.Second); err != nil {
		t.Fatalf("business runtime did not become ready: %v", err)
	}

	owner, ownerCookie := registerHTTPIntegrationUser(t, router, ownerName)
	_, actorCookie := registerHTTPIntegrationUser(t, router, actorName)
	created := performJSONRequest(router, stdhttp.MethodPost, "/api/v1/posts", `{"title":"native business flow","content":"outbox worker search"}`, ownerCookie)
	if created.Code != stdhttp.StatusCreated {
		t.Fatalf("create post status=%d body=%s", created.Code, created.Body.String())
	}
	var createdEnvelope struct {
		Data post.Post `json:"data"`
	}
	decodeIntegrationResponse(t, created, &createdEnvelope)
	postPath := "/api/v1/posts/" + strconv.FormatUint(createdEnvelope.Data.ID, 10)

	commentResponse := performJSONRequest(router, stdhttp.MethodPost, postPath+"/comments", `{"content":"native comment notification"}`, actorCookie)
	if commentResponse.Code != stdhttp.StatusCreated {
		t.Fatalf("create comment status=%d body=%s", commentResponse.Code, commentResponse.Body.String())
	}
	firstLike := performJSONRequest(router, stdhttp.MethodPut, postPath+"/like", "", actorCookie)
	if firstLike.Code != stdhttp.StatusNoContent {
		t.Fatalf("first like status=%d body=%s", firstLike.Code, firstLike.Body.String())
	}
	duplicateLike := performJSONRequest(router, stdhttp.MethodPut, postPath+"/like", "", actorCookie)
	if duplicateLike.Code != stdhttp.StatusNoContent {
		t.Fatalf("duplicate like status=%d body=%s, want idempotent 204", duplicateLike.Code, duplicateLike.Body.String())
	}

	if !waitUntilIntegration(45*time.Second, func() bool {
		var count int
		return database.QueryRow(`SELECT COUNT(*) FROM notifications WHERE recipient_id=? AND post_id=?`, owner.ID, createdEnvelope.Data.ID).Scan(&count) == nil && count == 2
	}) {
		logBusinessFlowDiagnostics(t, database, owner.ID, createdEnvelope.Data.ID)
		t.Fatal("native business flow notifications did not converge within the bounded wait")
	}
	var notificationCount, commentNotifications, likeNotifications int
	if err := database.QueryRow(`SELECT COUNT(*), SUM(type='comment.created'), SUM(type='post.liked') FROM notifications WHERE recipient_id=? AND post_id=?`, owner.ID, createdEnvelope.Data.ID).Scan(&notificationCount, &commentNotifications, &likeNotifications); err != nil {
		t.Fatal(err)
	}
	if notificationCount != 2 || commentNotifications != 1 || likeNotifications != 1 {
		t.Fatalf("notification fanout count=%d comment=%d like=%d, want one of each", notificationCount, commentNotifications, likeNotifications)
	}

	if !waitUntilIntegration(45*time.Second, func() bool {
		searchResponse := performJSONRequest(router, stdhttp.MethodGet, "/api/v1/search/posts?q=native+business+flow", "", actorCookie)
		return searchResponse.Code == stdhttp.StatusOK && searchResponseContainsPost(searchResponse.Body.Bytes(), createdEnvelope.Data.ID)
	}) {
		logBusinessFlowDiagnostics(t, database, owner.ID, createdEnvelope.Data.ID)
		t.Fatal("native business flow search did not converge within the bounded wait")
	}
	searchResponse := performJSONRequest(router, stdhttp.MethodGet, "/api/v1/search/posts?q=native+business+flow", "", actorCookie)
	if searchResponse.Code != stdhttp.StatusOK || !searchResponseContainsPost(searchResponse.Body.Bytes(), createdEnvelope.Data.ID) {
		t.Fatalf("search status=%d body=%s", searchResponse.Code, searchResponse.Body.String())
	}
	t.Logf("native business flow passed post_id=%d recipient=%d notifications=%d duplicate_like_status=%d", createdEnvelope.Data.ID, owner.ID, notificationCount, duplicateLike.Code)
}

func integrationBusinessRouter(t *testing.T, cfg config.Config, database *sql.DB) (stdhttp.Handler, func(), func(time.Duration) error) {
	t.Helper()
	state, err := businessstate.NewRepository(database)
	if err != nil {
		t.Fatalf("business state repository error = %v", err)
	}
	if _, err := state.Ensure(context.Background()); err != nil {
		t.Fatalf("business state ensure error = %v", err)
	}
	if err := state.ActivateV2(context.Background(), "phase-24-02-business-flow"); err != nil {
		t.Fatalf("business state activation error = %v", err)
	}
	eventOutbox, err := outbox.NewRepository(database, outbox.Options{MaxClaimBatch: 5, RetentionAccounting: state, EnableV2: true})
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	rabbitPublisher, err := platform.NewRabbitMQPublisher(cfg.RabbitMQURL, platform.RabbitMQPublisherOptions{RetryDelay: cfg.Outbox.RetryDelay, SearchRetryDelay: cfg.Outbox.SearchRetryDelay})
	if err != nil {
		t.Fatalf("NewRabbitMQPublisher() error = %v", err)
	}
	redisClient := platform.NewRedis(cfg.Redis)
	searchClient, err := platform.NewElasticsearch(cfg.Elasticsearch)
	if err != nil {
		t.Fatalf("NewElasticsearch() error = %v", err)
	}

	dispatcher, err := outbox.NewDispatcher(eventOutbox, rabbitPublisher, outbox.DispatcherOptions{Owner: "native-business-dispatcher", PollInterval: 20 * time.Millisecond, ClaimBatch: 5, LeaseDuration: 30 * time.Second, PublishTimeout: cfg.Outbox.PublishTimeout, CleanupInterval: time.Minute, Retention: time.Hour, CleanupBatch: 20})
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	notificationRepository, err := notification.NewRepository(database)
	if err != nil {
		t.Fatalf("notification repository error = %v", err)
	}
	notificationProcessor, err := notification.NewProcessor(notificationRepository)
	if err != nil {
		t.Fatalf("notification processor error = %v", err)
	}
	workerRuntime, err := worker.NewRuntime(cfg.RabbitMQURL, notificationProcessor, worker.RuntimeOptions{Profile: worker.BusinessProfile, InstanceID: "business-test", Admission: state, Prefetch: 4, MaxRetries: 2, RetryDelay: cfg.Outbox.RetryDelay, PublishTimeout: cfg.Outbox.PublishTimeout, ShutdownTimeout: 5 * time.Second, ReconnectMinimum: 100 * time.Millisecond, ReconnectMaximum: time.Second})
	if err != nil {
		t.Fatalf("business worker runtime error = %v", err)
	}
	searchProcessor, err := search.NewProcessor(search.NewMySQLDocumentStore(database), search.NewElasticsearchRepository(searchClient))
	if err != nil {
		t.Fatalf("search processor error = %v", err)
	}
	searchProcessor.WithBusinessState(state).WithLogger(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	searchRuntime, err := worker.NewRuntime(cfg.RabbitMQURL, integrationDiagnosticProcessor{delegate: searchProcessor}, worker.RuntimeOptions{Profile: worker.SearchProfile, InstanceID: "search-test", Admission: state, Prefetch: 4, MaxRetries: 2, RetryDelay: cfg.Outbox.SearchRetryDelay, PublishTimeout: cfg.Outbox.PublishTimeout, ShutdownTimeout: 5 * time.Second, ReconnectMinimum: 100 * time.Millisecond, ReconnectMaximum: time.Second})
	if err != nil {
		t.Fatalf("search worker runtime error = %v", err)
	}

	passwords, err := auth.NewPasswordManager()
	if err != nil {
		t.Fatalf("password manager error = %v", err)
	}
	tokens, err := auth.NewTokenManager(cfg.Auth.JWTSecret, cfg.Auth.JWTTTL, time.Now)
	if err != nil {
		t.Fatalf("token manager error = %v", err)
	}
	cookies := auth.NewCookieManager(cfg.Auth.CookieName, cfg.Auth.CookieSecure, cfg.Auth.JWTTTL, time.Now)
	users := user.NewMySQLRepository(database).WithOutbox(eventOutbox)
	authService := auth.NewService(users, passwords, tokens)
	postCache := rediscache.NewPostBodyRepository(redisClient, cfg.Redis.PostDetailTTL, cfg.Redis.OperationTimeout)
	posts := post.NewMySQLRepository(database, post.RepositoryOptions{Outbox: eventOutbox, BusinessState: state})
	postService := post.NewService(posts).WithBodyCache(postCache, nil)
	t.Cleanup(func() { _ = postService.Shutdown(context.Background()) })
	comments := comment.NewMySQLRepositoryWithOutbox(database, eventOutbox)
	likes := like.NewMySQLRepositoryWithOutbox(database, eventOutbox)
	commentService := comment.NewService(comments, postService)
	likeService := like.NewService(likes, postService)
	searchService := search.NewService(search.NewElasticsearchRepository(searchClient), posts, cfg.Auth.JWTSecret).WithEpochProvider(state)
	router := NewRouter(Dependencies{}, APIRoutes{Auth: auth.NewHandler(authService, cookies), Posts: post.NewHandler(postService), Comments: comment.NewHandler(commentService), Likes: like.NewHandler(likeService), Notifications: notification.NewHandler(notification.NewService(notificationRepository)), Search: search.NewHandler(searchService), Authentication: middleware.RequireAuthentication(cookies.Name(), tokens)})

	ctx, cancel := context.WithCancel(context.Background())
	runtimeErrors := make(chan error, 3)
	var runtimeGroup sync.WaitGroup
	for name, run := range map[string]func(context.Context) error{
		"dispatcher":      dispatcher.Run,
		"business worker": workerRuntime.Run,
		"search worker":   searchRuntime.Run,
	} {
		runtimeGroup.Add(1)
		go func(name string, run func(context.Context) error) {
			defer runtimeGroup.Done()
			if err := run(ctx); err != nil && ctx.Err() == nil {
				runtimeErrors <- fmt.Errorf("%s: %w", name, err)
			}
		}(name, run)
	}
	waitRuntime := func(timeout time.Duration) error {
		if timeout <= 0 {
			return fmt.Errorf("runtime readiness timeout must be positive")
		}
		deadline := time.Now().Add(timeout)
		for {
			select {
			case err := <-runtimeErrors:
				return err
			default:
			}
			if workerRuntime.Ready(context.Background()) == nil && searchRuntime.Ready(context.Background()) == nil {
				return nil
			}
			if !time.Now().Before(deadline) {
				return fmt.Errorf("business worker ready=%v, search worker ready=%v", workerRuntime.Ready(context.Background()), searchRuntime.Ready(context.Background()))
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	stopRuntime := func() {
		cancel()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if err := rabbitPublisher.Close(); err == nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		done := make(chan struct{})
		go func() {
			runtimeGroup.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
		_ = redisClient.Close()
	}
	return router, stopRuntime, waitRuntime
}

type integrationDiagnosticProcessor struct {
	delegate worker.Processor
}

func (processor integrationDiagnosticProcessor) Process(ctx context.Context, envelope bus.Envelope) error {
	err := processor.delegate.Process(ctx, envelope)
	if err != nil {
		log.Printf("business flow search processing failed event_id=%s post_id=%d error=%v", envelope.EventID, envelope.PostID, err)
	}
	return err
}

func waitUntilIntegration(timeout time.Duration, check func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func logBusinessFlowDiagnostics(t *testing.T, database *sql.DB, ownerID, postID uint64) {
	t.Helper()
	rows, err := database.Query(`
		SELECT o.id, o.event_id, o.event_type, o.schema_version, o.status, o.attempt_count, COALESCE(o.last_error, ''),
		       COALESCE(r.consumer, ''), IF(r.closed_at IS NULL, 0, 1), COALESCE(a.result, '')
		FROM business_outbox AS o
		LEFT JOIN business_event_requirements AS r ON r.data_epoch=? AND r.event_id=o.event_id
		LEFT JOIN business_application_results AS a ON a.data_epoch=r.data_epoch AND a.event_id=r.event_id AND a.consumer=r.consumer AND a.routing_contract_revision=r.routing_contract_revision
		WHERE JSON_UNQUOTE(JSON_EXTRACT(o.payload, '$.actor_id'))=? OR JSON_UNQUOTE(JSON_EXTRACT(o.payload, '$.post_id'))=?
		ORDER BY o.id`, currentBusinessFlowEpoch(t, database), ownerID, postID)
	if err != nil {
		t.Logf("business flow diagnostics outbox query error=%v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id uint64
		var schemaVersion, attempt, closed int
		var eventID, eventType, status, lastError, consumer, result string
		if err := rows.Scan(&id, &eventID, &eventType, &schemaVersion, &status, &attempt, &lastError, &consumer, &closed, &result); err != nil {
			t.Logf("business flow diagnostics outbox scan error=%v", err)
			return
		}
		t.Logf("business flow outbox id=%d event_id=%s type=%s schema=%d status=%s attempts=%d last_error=%s consumer=%s closed=%d application=%s", id, eventID, eventType, schemaVersion, status, attempt, lastError, consumer, closed, result)
	}
	if err := rows.Err(); err != nil {
		t.Logf("business flow diagnostics outbox rows error=%v", err)
	}
	failureRows, err := database.Query(`
		SELECT f.event_id, f.consumer, f.attempt_count, f.retry_count, f.dead_count, f.last_reason
		FROM business_event_failures AS f
		INNER JOIN business_outbox AS o ON o.event_id=f.event_id
		WHERE JSON_UNQUOTE(JSON_EXTRACT(o.payload, '$.actor_id'))=? OR JSON_UNQUOTE(JSON_EXTRACT(o.payload, '$.post_id'))=?`, ownerID, postID)
	if err == nil {
		defer failureRows.Close()
		for failureRows.Next() {
			var eventID, consumer, reason string
			var attempts, retries, dead int
			if scanErr := failureRows.Scan(&eventID, &consumer, &attempts, &retries, &dead, &reason); scanErr != nil {
				t.Logf("business flow diagnostics failure scan error=%v", scanErr)
				break
			}
			t.Logf("business flow failure event_id=%s consumer=%s attempts=%d retries=%d dead=%d reason=%s", eventID, consumer, attempts, retries, dead, reason)
		}
	} else {
		t.Logf("business flow diagnostics failure query error=%v", err)
	}
	var parked, failed int
	_ = database.QueryRow(`SELECT COUNT(*) FROM business_message_parking WHERE event_id IN (SELECT event_id FROM business_outbox WHERE JSON_UNQUOTE(JSON_EXTRACT(payload, '$.actor_id'))=?)`, ownerID).Scan(&parked)
	_ = database.QueryRow(`SELECT COUNT(*) FROM business_event_failures WHERE event_id IN (SELECT event_id FROM business_outbox WHERE JSON_UNQUOTE(JSON_EXTRACT(payload, '$.actor_id'))=?)`, ownerID).Scan(&failed)
	t.Logf("business flow parked=%d failures=%d", parked, failed)
}

func currentBusinessFlowEpoch(t *testing.T, database *sql.DB) string {
	t.Helper()
	var epoch string
	if err := database.QueryRow(`SELECT data_epoch FROM business_environment WHERE singleton=1`).Scan(&epoch); err != nil {
		t.Logf("business flow diagnostics epoch query error=%v", err)
	}
	return epoch
}

func searchResponseContainsPost(body []byte, postID uint64) bool {
	var envelope struct {
		Data []struct {
			ID uint64 `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return false
	}
	for _, item := range envelope.Data {
		if item.ID == postID {
			return true
		}
	}
	return false
}

func cleanupBusinessFlowData(t *testing.T, cfg config.Config, epoch string, baseline businessstate.Budget, usernames ...string) {
	t.Helper()
	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Errorf("business flow cleanup OpenMySQLDatabase() error = %v", err)
		return
	}
	defer database.Close()
	var ownerID, actorID uint64
	if err := database.QueryRowContext(context.Background(), `SELECT id FROM users WHERE username=?`, usernames[0]).Scan(&ownerID); err != nil {
		if err == sql.ErrNoRows {
			return
		}
		t.Errorf("business flow cleanup owner lookup: %v", err)
		return
	}
	if err := database.QueryRowContext(context.Background(), `SELECT id FROM users WHERE username=?`, usernames[1]).Scan(&actorID); err != nil {
		if err == sql.ErrNoRows {
			return
		}
		t.Errorf("business flow cleanup actor lookup: %v", err)
		return
	}
	arguments := []any{usernames[0], usernames[1], usernames[0], usernames[1]}
	query := `DELETE n FROM notifications AS n LEFT JOIN users AS recipient ON recipient.id=n.recipient_id LEFT JOIN users AS actor ON actor.id=n.actor_id WHERE recipient.username IN (?, ?) OR actor.username IN (?, ?)`
	if _, err := database.ExecContext(context.Background(), query, arguments...); err != nil {
		t.Errorf("business flow cleanup notifications: %v", err)
	}
	for _, cleanup := range []struct {
		query string
		args  []any
	}{
		{query: `DELETE FROM business_application_results WHERE data_epoch=? AND event_id IN (SELECT event_id FROM business_commands WHERE data_epoch=? AND user_id IN (?, ?))`, args: []any{epoch, epoch, ownerID, actorID}},
		{query: `DELETE FROM business_event_requirements WHERE data_epoch=? AND event_id IN (SELECT event_id FROM business_commands WHERE data_epoch=? AND user_id IN (?, ?))`, args: []any{epoch, epoch, ownerID, actorID}},
		{query: `DELETE FROM business_outbox WHERE JSON_UNQUOTE(JSON_EXTRACT(payload, '$.actor_id')) IN (?, ?)`, args: []any{ownerID, actorID}},
		{query: `DELETE FROM business_commands WHERE data_epoch=? AND user_id IN (?, ?)`, args: []any{epoch, ownerID, actorID}},
	} {
		if _, err := database.ExecContext(context.Background(), cleanup.query, cleanup.args...); err != nil {
			t.Errorf("business flow cleanup command data: %v", err)
		}
	}
	if _, err := database.ExecContext(context.Background(), `UPDATE business_async_budget SET open_requirements=?, retained_groups=?, retained_bytes=?, parked_messages=?, parked_bytes=? WHERE data_epoch=?`, baseline.OpenRequirements, baseline.RetainedGroups, baseline.RetainedBytes, baseline.ParkedMessages, baseline.ParkedBytes, epoch); err != nil {
		t.Errorf("business flow cleanup budget: %v", err)
	}
}
