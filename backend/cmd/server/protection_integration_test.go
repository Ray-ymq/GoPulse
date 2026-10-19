//go:build integration

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/instrumentation"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	rediscache "github.com/Ray-ymq/GoPulse/backend/internal/platform/redis"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/Ray-ymq/GoPulse/backend/internal/ratelimit"
)

// TestIntegrationRedisProtectionAndVersionedBody exercises the enabled path
// with two independently assembled business processes. It intentionally keeps
// the assertions at the protocol and correctness boundary; the performance
// thresholds are recorded by the separate Phase 1/3 evidence run.
func TestIntegrationRedisProtectionAndVersionedBody(t *testing.T) {
	backendDir := findBackendDir(t)
	base := integrationEnvironment(t)
	setEnvironment(&base, "BACKEND_RATE_LIMIT_ENABLED", "true")
	setEnvironment(&base, "BACKEND_TRUSTED_PROXY_CIDRS", "127.0.0.1/32")
	setEnvironment(&base, "BACKEND_RESOURCE_READ", "128")
	setEnvironment(&base, "BACKEND_RESOURCE_DATABASE", "32")
	setEnvironment(&base, "BACKEND_RESOURCE_REBUILD", "8")
	setEnvironment(&base, "BACKEND_RESOURCE_WAITER", "256")
	runIntegrationCommand(t, backendDir, integrationCommandTimeout, base, "go", "run", "./cmd/migrate", "up")

	metrics, err := instrumentation.NewBackend(instrumentation.BackendRoutes())
	if err != nil {
		t.Fatalf("create protection metrics: %v", err)
	}
	instrumentation.InstallBackend(metrics)
	defer instrumentation.InstallBackend(nil)

	clientJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create protection cookie jar: %v", err)
	}
	client := &http.Client{Jar: clientJar, Timeout: 5 * time.Second}

	server1 := startProtectionAssembly(t, base, "backend-protection-1", "")
	server2 := startProtectionAssembly(t, base, "backend-protection-2", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load protection integration configuration: %v", err)
	}
	redisProbe := platform.NewRedis(cfg.Redis)
	defer redisProbe.Close()
	if err := redisProbe.Check(context.Background()); err != nil {
		t.Fatalf("check protection Redis: %v", err)
	}
	probePolicy := protectionPolicies(cfg.Redis.PostDetailTTL)["auth_source"]
	if _, err := ratelimit.NewSharedLimiter(redisProbe, cfg.Redis.OperationTimeout).Allow(context.Background(), probePolicy, "ip:protection-probe"); err != nil {
		t.Fatalf("execute protection Redis quota script: %v (policy=%+v)", err, probePolicy)
	}
	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("open protection integration database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	releaseFactsLock := integrationtest.AcquirePostFactsLock(t, database)
	defer releaseFactsLock()

	suffix := strconv.FormatInt(time.Now().UnixNano()%1000000000, 10)
	username := "RedisProtection_" + suffix
	password := "redis-protection-password"
	userID := registerIntegrationUser(t, client, server1.address, username, password)
	postID := createProtectionPost(t, client, server1.address, "v2 original "+suffix, "body before edit")
	t.Cleanup(func() { cleanupProtectionFixture(t, database, userID, postID) })

	first := readProtectionPost(t, client, server1.address, postID)
	if first.Title != "v2 original "+suffix || first.ContentRevision != 1 {
		t.Fatalf("cold v2 detail = %#v", first)
	}
	second := readProtectionPost(t, client, server2.address, postID)
	if second.Title != first.Title || second.Content != first.Content {
		t.Fatalf("cross-instance warm v2 detail = %#v, first=%#v", second, first)
	}

	if status, body := protectionRequest(t, client, http.MethodPut, server2.address+"/api/v1/posts/"+strconv.FormatUint(postID, 10)+"/like", nil); status != http.StatusNoContent {
		t.Fatalf("like status=%d body=%s", status, body)
	}
	if status, body := protectionRequest(t, client, http.MethodPost, server2.address+"/api/v1/posts/"+strconv.FormatUint(postID, 10)+"/comments", map[string]string{"content": "v2 interaction"}); status != http.StatusCreated {
		t.Fatalf("comment status=%d body=%s", status, body)
	}
	interacted := readProtectionPost(t, client, server1.address, postID)
	if interacted.Title != first.Title || interacted.Content != first.Content || interacted.LikeCount != 1 || interacted.CommentCount != 1 {
		t.Fatalf("interaction must keep v2 body and refresh dynamics = %#v", interacted)
	}

	brokenRedis := startProtectionAssembly(t, base, "backend-protection-broken-redis", "1")
	degraded := readProtectionPost(t, client, brokenRedis.address, postID)
	if degraded.Title != first.Title || degraded.LikeCount != 1 || degraded.CommentCount != 1 {
		t.Fatalf("Redis outage read fallback = %#v", degraded)
	}
	status, body := protectionRequest(t, client, http.MethodPost, brokenRedis.address+"/api/v1/posts", map[string]string{"title": "must not commit", "content": "Redis unavailable"})
	if status != http.StatusServiceUnavailable || protectionErrorCode(body) != "rate_limit_unavailable" {
		t.Fatalf("Redis outage write status=%d code=%q body=%s", status, protectionErrorCode(body), body)
	}
	brokenRedis.close()

	if status, body := protectionRequest(t, client, http.MethodPatch, server1.address+"/api/v1/posts/"+strconv.FormatUint(postID, 10), map[string]string{"title": "v2 edited", "content": "body after edit"}); status != http.StatusOK {
		t.Fatalf("edit status=%d body=%s", status, body)
	}
	edited := readProtectionPost(t, client, server2.address, postID)
	if edited.Title != "v2 edited" || edited.Content != "body after edit" || edited.ContentRevision != 2 {
		t.Fatalf("edited v2 detail = %#v", edited)
	}
	if status, body := protectionRequest(t, client, http.MethodDelete, server1.address+"/api/v1/posts/"+strconv.FormatUint(postID, 10), nil); status != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", status, body)
	}
	if status, body := protectionRequest(t, client, http.MethodGet, server2.address+"/api/v1/posts/"+strconv.FormatUint(postID, 10), nil); status != http.StatusNotFound || protectionErrorCode(body) != "post_not_found" {
		t.Fatalf("deleted v2 detail status=%d code=%q body=%s", status, protectionErrorCode(body), body)
	}

	// The same authenticated identity is deliberately split across both
	// processes. A single shared post_read bucket must eventually reject on
	// either instance, not provide one independent allowance per process.
	var waitGroup sync.WaitGroup
	results := make(chan protectionResult, 160)
	start := make(chan struct{})
	for index := 0; index < cap(results); index++ {
		address := server1.address
		if index%2 == 1 {
			address = server2.address
		}
		waitGroup.Add(1)
		go func(address string) {
			defer waitGroup.Done()
			<-start
			status, body, requestErr := rawProtectionRequest(client, http.MethodGet, address+"/api/v1/posts/0", nil)
			results <- protectionResult{status: status, body: body, err: requestErr}
		}(address)
	}
	close(start)
	waitGroup.Wait()
	close(results)
	rateLimited := 0
	validationFailures := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("shared rate limit request: %v", result.err)
		}
		switch result.status {
		case http.StatusTooManyRequests:
			rateLimited++
		case http.StatusBadRequest:
			validationFailures++
		default:
			t.Fatalf("shared rate limit status=%d body=%s", result.status, result.body)
		}
	}
	if rateLimited == 0 || validationFailures == 0 {
		t.Fatalf("shared rate limit did not cross both instances: rate_limited=%d validation=%d", rateLimited, validationFailures)
	}

}

func TestIntegrationRateLimitDisabledKeepsVersionedBodyCache(t *testing.T) {
	backendDir := findBackendDir(t)
	base := integrationEnvironment(t)
	setEnvironment(&base, "BACKEND_RATE_LIMIT_ENABLED", "false")
	runIntegrationCommand(t, backendDir, integrationCommandTimeout, base, "go", "run", "./cmd/migrate", "up")

	metrics, err := instrumentation.NewBackend(instrumentation.BackendRoutes())
	if err != nil {
		t.Fatalf("create body metrics: %v", err)
	}
	instrumentation.InstallBackend(metrics)
	defer instrumentation.InstallBackend(nil)

	clientJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create body cookie jar: %v", err)
	}
	client := &http.Client{Jar: clientJar, Timeout: 5 * time.Second}
	server := startProtectionAssembly(t, base, "backend-body-path", "")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load body configuration: %v", err)
	}
	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("open body database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	releaseFactsLock := integrationtest.AcquirePostFactsLock(t, database)
	defer releaseFactsLock()

	suffix := strconv.FormatInt(time.Now().UnixNano()%1000000000, 10)
	userID := registerIntegrationUser(t, client, server.address, "BodyPath_"+suffix, "body-path-password")
	postID := createProtectionPost(t, client, server.address, "body title "+suffix, "body content")
	t.Cleanup(func() { cleanupProtectionFixture(t, database, userID, postID) })

	first := readProtectionPost(t, client, server.address, postID)
	second := readProtectionPost(t, client, server.address, postID)
	if first.Title != second.Title || first.Content != second.Content || first.ContentRevision != 1 {
		t.Fatalf("body detail changed between reads first=%#v second=%#v", first, second)
	}

	redisClient := platform.NewRedis(cfg.Redis)
	defer redisClient.Close()
	var epoch string
	if err := database.QueryRowContext(context.Background(), "SELECT data_epoch FROM business_environment WHERE singleton=1").Scan(&epoch); err != nil {
		t.Fatalf("read body cache epoch: %v", err)
	}
	payload, err := redisClient.Get(context.Background(), rediscache.PostBodyKey(post.BodyKey{Epoch: epoch, PostID: postID, ContentRevision: 1}))
	if err != nil {
		t.Fatalf("versioned body cache read: %v", err)
	}
	if !strings.Contains(payload, `"version":2`) || !strings.Contains(payload, "body title "+suffix) {
		t.Fatalf("versioned body cache payload=%s", payload)
	}
	for _, oldKey := range []string{fmt.Sprintf("gopulse:post:detail:v1:%d", postID), fmt.Sprintf("gopulse:post:detail:v1:epoch:%s:%d", epoch, postID)} {
		if _, err := redisClient.Get(context.Background(), oldKey); !errors.Is(err, platform.ErrRedisKeyNotFound) {
			t.Fatalf("retired cache key %s exists: %v", oldKey, err)
		}
	}
}

type protectionPost struct {
	ID              uint64 `json:"id"`
	Title           string `json:"title"`
	Content         string `json:"content"`
	ContentRevision uint64 `json:"content_revision"`
	CommentCount    uint64 `json:"comment_count"`
	LikeCount       uint64 `json:"like_count"`
}

type protectionResult struct {
	status int
	body   string
	err    error
}

type protectionInstance struct {
	server   *httptest.Server
	assembly *roleAssembly
	name     string
	address  string
	closed   bool
}

func startProtectionAssembly(t *testing.T, base []string, instance, redisPort string) *protectionInstance {
	t.Helper()
	environment := roleEnvironment(base, "business", 128)
	setEnvironment(&environment, "GOPULSE_INSTANCE_ID", instance)
	if redisPort != "" {
		setEnvironment(&environment, "REDIS_PORT", redisPort)
	}
	values := make(map[string]string, len(environment))
	for _, entry := range environment {
		key, value, found := strings.Cut(entry, "=")
		if found {
			values[key] = value
		}
	}
	cfg, err := config.LoadFrom(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err != nil {
		t.Fatalf("load protection instance configuration: %v", err)
	}
	var assembly *roleAssembly
	for attempt := 0; attempt < 3; attempt++ {
		assembly, err = newRoleAssembly(cfg, logging.Discard("backend"), context.Background())
		if err == nil {
			break
		}
		if !strings.Contains(err.Error(), "invalid connection") || attempt == 2 {
			t.Fatalf("assemble protection instance %s: %v", instance, err)
		}
		time.Sleep(250 * time.Millisecond)
	}
	instanceServer := &protectionInstance{server: httptest.NewServer(assembly.router), assembly: assembly, name: "server-1"}
	if strings.HasSuffix(instance, "-2") {
		instanceServer.name = "server-2"
	}
	instanceServer.address = instanceServer.server.URL
	t.Cleanup(instanceServer.close)
	return instanceServer
}

func (instance *protectionInstance) close() {
	if instance == nil || instance.closed {
		return
	}
	instance.closed = true
	if instance.server != nil {
		instance.server.Close()
	}
	if instance.assembly != nil {
		instance.assembly.close(logging.Discard("backend"))
	}
}

func createProtectionPost(t *testing.T, client *http.Client, address, title, content string) uint64 {
	t.Helper()
	status, body := protectionRequest(t, client, http.MethodPost, address+"/api/v1/posts", map[string]string{"title": title, "content": content})
	if status != http.StatusCreated {
		t.Fatalf("create protection post status=%d body=%s", status, body)
	}
	var envelope struct {
		Data protectionPost `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil || envelope.Data.ID == 0 {
		t.Fatalf("decode created protection post: error=%v body=%s", err, body)
	}
	return envelope.Data.ID
}

func readProtectionPost(t *testing.T, client *http.Client, address string, postID uint64) protectionPost {
	t.Helper()
	status, body := protectionRequest(t, client, http.MethodGet, address+"/api/v1/posts/"+strconv.FormatUint(postID, 10), nil)
	if status != http.StatusOK {
		t.Fatalf("read protection post status=%d body=%s", status, body)
	}
	var envelope struct {
		Data protectionPost `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("decode protection detail: %v body=%s", err, body)
	}
	return envelope.Data
}

func protectionRequest(t *testing.T, client *http.Client, method, address string, payload any) (int, string) {
	t.Helper()
	status, body, err := rawProtectionRequest(client, method, address, payload)
	if err != nil {
		t.Fatalf("%s %s: %v", method, address, err)
	}
	return status, body
}

func rawProtectionRequest(client *http.Client, method, address string, payload any) (int, string, error) {
	var reader io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return 0, "", err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, address, reader)
	if err != nil {
		return 0, "", err
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, "", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if err != nil {
		return response.StatusCode, "", err
	}
	return response.StatusCode, string(body), nil
}

func protectionErrorCode(body string) string {
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		return ""
	}
	return envelope.Error.Code
}

func cleanupProtectionFixture(t *testing.T, database *sql.DB, userID, postID uint64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{query: "DELETE FROM post_bookmarks WHERE post_id=? OR user_id=?", args: []any{postID, userID}},
		{query: "DELETE FROM user_follows WHERE follower_id=? OR followed_id=?", args: []any{userID, userID}},
		{query: "DELETE FROM post_likes WHERE post_id=? OR user_id=?", args: []any{postID, userID}},
		{query: "DELETE FROM comments WHERE post_id=? OR author_id=?", args: []any{postID, userID}},
		{query: "DELETE FROM posts WHERE id=?", args: []any{postID}},
		{query: "DELETE FROM users WHERE id=?", args: []any{userID}},
	} {
		if _, err := database.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Errorf("cleanup protection fixture: %v", err)
		}
	}
}
