//go:build integration

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/logging"
)

// TestIntegrationRedisProtectionAndVersionedBody exercises the enabled path
// with two independently assembled business processes. It intentionally keeps
// the assertions at the protocol and correctness boundary; the performance
// thresholds are recorded by the separate Phase 1/3 evidence run.

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

func TestIntegrationReadinessUsesDedicatedPoolWhenAPIPoolExhausted(t *testing.T) {
	base := integrationEnvironment(t)
	setEnvironment(&base, "MYSQL_MAX_OPEN_CONNS", "8")
	instance := startProtectionAssembly(t, base, "backend-probe-isolation", "")
	instance.assembly.probes.Started()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connections := []*sql.Conn{}
	defer func() {
		for _, conn := range connections {
			conn.Close()
		}
	}()
	for i := 0; i < 8; i++ {
		conn, err := instance.assembly.database.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, conn)
	}
	if got := instance.assembly.pools["probe"].Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("probe pool=%d", got)
	}
	if instance.assembly.pools["probe"] == instance.assembly.database || instance.assembly.pools["outbox"] == instance.assembly.database {
		t.Fatal("pools are shared")
	}
	// Every request is allowed to expire the probe cache first.
	for i := 0; i < 2; i++ {
		time.Sleep(300 * time.Millisecond)
		response, err := http.Get(instance.address + "/ready")
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("ready status=%d with API pool saturated", response.StatusCode)
		}
	}
}
