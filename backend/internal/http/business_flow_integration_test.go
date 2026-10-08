//go:build integration

package http

import (
	"context"
	"database/sql"
	"encoding/json"
	stdhttp "net/http"
	"strconv"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/auth"
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

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	if len(suffix) > 10 {
		suffix = suffix[len(suffix)-10:]
	}
	ownerName := "FlowOwner_" + suffix
	actorName := "FlowActor_" + suffix
	defer cleanupHTTPPostUsers(t, cfg, ownerName, actorName)
	defer cleanupBusinessFlowData(t, cfg, ownerName, actorName)
	router, stopRuntime := integrationBusinessRouter(t, cfg, database)
	defer stopRuntime()

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

	waitUntilIntegration(t, 45*time.Second, func() bool {
		var count int
		return database.QueryRow(`SELECT COUNT(*) FROM notifications WHERE recipient_id=? AND post_id=?`, owner.ID, createdEnvelope.Data.ID).Scan(&count) == nil && count == 2
	})
	var notificationCount, commentNotifications, likeNotifications int
	if err := database.QueryRow(`SELECT COUNT(*), SUM(type='comment.created'), SUM(type='post.liked') FROM notifications WHERE recipient_id=? AND post_id=?`, owner.ID, createdEnvelope.Data.ID).Scan(&notificationCount, &commentNotifications, &likeNotifications); err != nil {
		t.Fatal(err)
	}
	if notificationCount != 2 || commentNotifications != 1 || likeNotifications != 1 {
		t.Fatalf("notification fanout count=%d comment=%d like=%d, want one of each", notificationCount, commentNotifications, likeNotifications)
	}

	waitUntilIntegration(t, 45*time.Second, func() bool {
		searchResponse := performJSONRequest(router, stdhttp.MethodGet, "/api/v1/search/posts?q=native+business+flow", "", actorCookie)
		return searchResponse.Code == stdhttp.StatusOK && searchResponseContainsPost(searchResponse.Body.Bytes(), createdEnvelope.Data.ID)
	})
	searchResponse := performJSONRequest(router, stdhttp.MethodGet, "/api/v1/search/posts?q=native+business+flow", "", actorCookie)
	if searchResponse.Code != stdhttp.StatusOK || !searchResponseContainsPost(searchResponse.Body.Bytes(), createdEnvelope.Data.ID) {
		t.Fatalf("search status=%d body=%s", searchResponse.Code, searchResponse.Body.String())
	}
	t.Logf("native business flow passed post_id=%d recipient=%d notifications=%d duplicate_like_status=%d", createdEnvelope.Data.ID, owner.ID, notificationCount, duplicateLike.Code)
}

func integrationBusinessRouter(t *testing.T, cfg config.Config, database *sql.DB) (stdhttp.Handler, func()) {
	t.Helper()
	eventOutbox, err := outbox.NewRepository(database, outbox.Options{MaxClaimBatch: 5})
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	rabbitPublisher, err := platform.NewRabbitMQPublisher(cfg.RabbitMQURL, platform.RabbitMQPublisherOptions{RetryDelay: time.Second, SearchRetryDelay: time.Second})
	if err != nil {
		t.Fatalf("NewRabbitMQPublisher() error = %v", err)
	}
	redisClient := platform.NewRedis(cfg.Redis)
	searchClient, err := platform.NewElasticsearch(cfg.Elasticsearch)
	if err != nil {
		t.Fatalf("NewElasticsearch() error = %v", err)
	}

	dispatcher, err := outbox.NewDispatcher(eventOutbox, rabbitPublisher, outbox.DispatcherOptions{Owner: "native-business-dispatcher", PollInterval: 20 * time.Millisecond, ClaimBatch: 5, LeaseDuration: 15 * time.Second, PublishTimeout: time.Second, CleanupInterval: time.Minute, Retention: time.Hour, CleanupBatch: 20})
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
	workerRuntime, err := worker.NewRuntime(cfg.RabbitMQURL, notificationProcessor, worker.RuntimeOptions{Profile: worker.BusinessProfile, InstanceID: "business-test", Prefetch: 4, MaxRetries: 2, RetryDelay: time.Second, PublishTimeout: time.Second, ShutdownTimeout: 5 * time.Second, ReconnectMinimum: 100 * time.Millisecond, ReconnectMaximum: time.Second})
	if err != nil {
		t.Fatalf("business worker runtime error = %v", err)
	}
	searchProcessor, err := search.NewProcessor(search.NewMySQLDocumentStore(database), search.NewElasticsearchRepository(searchClient))
	if err != nil {
		t.Fatalf("search processor error = %v", err)
	}
	searchRuntime, err := worker.NewRuntime(cfg.RabbitMQURL, searchProcessor, worker.RuntimeOptions{Profile: worker.SearchProfile, InstanceID: "search-test", Prefetch: 4, MaxRetries: 2, RetryDelay: time.Second, PublishTimeout: time.Second, ShutdownTimeout: 5 * time.Second, ReconnectMinimum: 100 * time.Millisecond, ReconnectMaximum: time.Second})
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
	users := user.NewMySQLRepository(database)
	authService := auth.NewService(users, passwords, tokens)
	postCache := rediscache.NewPostDetailRepository(redisClient, cfg.Redis.PostDetailTTL, cfg.Redis.OperationTimeout)
	posts := post.NewMySQLRepository(database, post.RepositoryOptions{Outbox: eventOutbox})
	postService := post.NewService(posts, postCache)
	comments := comment.NewMySQLRepositoryWithOutbox(database, eventOutbox)
	likes := like.NewMySQLRepositoryWithOutbox(database, eventOutbox)
	commentService := comment.NewService(comments, postService, postCache)
	likeService := like.NewService(likes, postService, postCache)
	searchService := search.NewService(search.NewElasticsearchRepository(searchClient), posts, cfg.Auth.JWTSecret)
	router := NewRouter(Dependencies{}, APIRoutes{Auth: auth.NewHandler(authService, cookies), Posts: post.NewHandler(postService), Comments: comment.NewHandler(commentService), Likes: like.NewHandler(likeService), Notifications: notification.NewHandler(notification.NewService(notificationRepository)), Search: search.NewHandler(searchService), Authentication: middleware.RequireAuthentication(cookies.Name(), tokens)})

	ctx, cancel := context.WithCancel(context.Background())
	for _, run := range []func(context.Context) error{dispatcher.Run, workerRuntime.Run, searchRuntime.Run} {
		go func(run func(context.Context) error) { _ = run(ctx) }(run)
	}
	return router, func() {
		cancel()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if err := rabbitPublisher.Close(); err == nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		_ = redisClient.Close()
	}
}

func waitUntilIntegration(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("native business flow did not converge within the bounded wait")
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

func cleanupBusinessFlowData(t *testing.T, cfg config.Config, usernames ...string) {
	t.Helper()
	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Errorf("business flow cleanup OpenMySQLDatabase() error = %v", err)
		return
	}
	defer database.Close()
	arguments := []any{usernames[0], usernames[1], usernames[0], usernames[1]}
	query := `DELETE n FROM notifications AS n LEFT JOIN users AS recipient ON recipient.id=n.recipient_id LEFT JOIN users AS actor ON actor.id=n.actor_id WHERE recipient.username IN (?, ?) OR actor.username IN (?, ?)`
	if _, err := database.ExecContext(context.Background(), query, arguments...); err != nil {
		t.Errorf("business flow cleanup notifications: %v", err)
	}
}
