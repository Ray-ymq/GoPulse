//go:build integration

package worker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/notification"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	amqp "github.com/rabbitmq/amqp091-go"
)

type flakyProcessor struct {
	delegate Processor
	failures atomic.Int32
	calls    atomic.Int32
}

func (processor *flakyProcessor) Process(ctx context.Context, envelope bus.Envelope) error {
	call := processor.calls.Add(1)
	if call <= processor.failures.Load() {
		return errors.New("temporary MySQL failure")
	}
	return processor.delegate.Process(ctx, envelope)
}

type alwaysFailProcessor struct{}

func (alwaysFailProcessor) Process(context.Context, bus.Envelope) error {
	return errors.New("temporary MySQL failure")
}

type blockingProcessor struct {
	delegate Processor
	started  chan struct{}
	release  chan struct{}
	stopped  chan struct{}
	once     sync.Once
	stopOnce sync.Once
}

func (processor *blockingProcessor) Process(ctx context.Context, envelope bus.Envelope) error {
	processor.once.Do(func() { close(processor.started) })
	defer processor.stopOnce.Do(func() { close(processor.stopped) })
	select {
	case <-processor.release:
		return processor.delegate.Process(ctx, envelope)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestIntegrationBusinessWorkerEndToEndRetryDeadAndShutdownRedelivery(t *testing.T) {
	cfg := integrationtest.Environment(t)
	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("OpenMySQLDatabase() error = %v", err)
	}
	defer database.Close()
	release := integrationtest.AcquirePostFactsLock(t, database)
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	actorID := workerInsertUser(t, ctx, database, "worker_actor")
	recipientID := workerInsertUser(t, ctx, database, "worker_recipient")
	postID := workerInsertPost(t, ctx, database, recipientID)
	commentID := workerInsertComment(t, ctx, database, postID, actorID)
	defer workerCleanup(t, database, postID, actorID, recipientID)
	repository, _ := notification.NewRepository(database)
	processor, _ := notification.NewProcessor(repository)
	purgeBusinessQueues(t, cfg.RabbitMQURL)

	runtime := newIntegrationRuntime(t, cfg.RabbitMQURL, processor, 2)
	stop, done := startRuntime(runtime)
	commentEvent, _ := bus.NewCommentCreated(time.Now().UTC().Truncate(time.Second), actorID, recipientID, postID, commentID)
	likeEvent, _ := bus.NewPostLiked(time.Now().UTC().Truncate(time.Second), actorID, recipientID, postID)
	publishEnvelope(t, cfg.RabbitMQURL, commentEvent)
	publishEnvelope(t, cfg.RabbitMQURL, commentEvent)
	publishEnvelope(t, cfg.RabbitMQURL, likeEvent)
	waitNotificationCount(t, ctx, database, postID, 2)

	publishInvalid(t, cfg.RabbitMQURL)
	waitQueueMessages(t, ctx, cfg.RabbitMQURL, platform.BusinessDeadQueue, 1)

	stop()
	waitRuntime(t, done)
	recoveryCommentID := workerInsertComment(t, ctx, database, postID, actorID)
	recoveryEvent, _ := bus.NewCommentCreated(time.Now().UTC().Truncate(time.Second), actorID, recipientID, postID, recoveryCommentID)
	blocked := &blockingProcessor{delegate: processor, started: make(chan struct{}), release: make(chan struct{}), stopped: make(chan struct{})}
	runtime = newIntegrationRuntime(t, cfg.RabbitMQURL, blocked, 2)
	stop, done = startRuntime(runtime)
	publishEnvelope(t, cfg.RabbitMQURL, recoveryEvent)
	select {
	case <-blocked.started:
	case <-ctx.Done():
		t.Fatal("worker did not start the shutdown-redelivery event")
	}
	stop()
	waitRuntime(t, done)
	select {
	case <-blocked.stopped:
	default:
		t.Fatal("runtime returned before the canceled processor stopped")
	}
	assertEventNotificationCount(t, ctx, database, recoveryEvent.EventID, 0)
	runtime = newIntegrationRuntime(t, cfg.RabbitMQURL, processor, 2)
	stop, done = startRuntime(runtime)
	waitEventNotification(t, ctx, database, recoveryEvent.EventID)
	stop()
	waitRuntime(t, done)

	flaky := &flakyProcessor{delegate: processor}
	flaky.failures.Store(2)
	retryEvent, _ := bus.NewPostLiked(time.Now().UTC().Truncate(time.Second), actorID, recipientID, postID)
	runtime = newIntegrationRuntime(t, cfg.RabbitMQURL, flaky, 3)
	stop, done = startRuntime(runtime)
	publishEnvelope(t, cfg.RabbitMQURL, retryEvent)
	waitEventNotification(t, ctx, database, retryEvent.EventID)
	if flaky.calls.Load() < 3 {
		t.Fatalf("flaky processor calls=%d want at least 3", flaky.calls.Load())
	}
	stop()
	waitRuntime(t, done)

	deadBefore := queueMessages(t, cfg.RabbitMQURL, platform.BusinessDeadQueue)
	failEvent, _ := bus.NewPostLiked(time.Now().UTC().Truncate(time.Second), actorID, recipientID, postID)
	runtime = newIntegrationRuntime(t, cfg.RabbitMQURL, alwaysFailProcessor{}, 1)
	stop, done = startRuntime(runtime)
	publishEnvelope(t, cfg.RabbitMQURL, failEvent)
	waitQueueMessages(t, ctx, cfg.RabbitMQURL, platform.BusinessDeadQueue, deadBefore+1)
	assertEventNotificationCount(t, ctx, database, failEvent.EventID, 0)
	stop()
	waitRuntime(t, done)
}

func newIntegrationRuntime(t *testing.T, url string, processor Processor, maxRetries int) *Runtime {
	t.Helper()
	runtime, err := NewRuntime(url, processor, RuntimeOptions{
		Prefetch: 4, MaxRetries: maxRetries, RetryDelay: time.Second, PublishTimeout: 3 * time.Second,
		ShutdownTimeout: 3 * time.Second, ReconnectMinimum: 100 * time.Millisecond, ReconnectMaximum: time.Second,
		Logger: func(string, ...any) {},
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	return runtime
}
func startRuntime(runtime *Runtime) (context.CancelFunc, <-chan error) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	return cancel, done
}
func waitRuntime(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Runtime.Run() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runtime did not stop")
	}
}
func publishEnvelope(t *testing.T, url string, envelope bus.Envelope) {
	t.Helper()
	body, _ := bus.Encode(envelope)
	metadata, _ := envelope.Metadata()
	routing, _ := envelope.RoutingKey()
	publishRaw(t, url, routing, amqp.Publishing{Headers: amqp.Table{}, ContentType: metadata.ContentType, DeliveryMode: amqp.Persistent, MessageId: metadata.MessageID, Timestamp: metadata.Timestamp, Type: metadata.Type, Body: body})
}
func publishInvalid(t *testing.T, url string) {
	t.Helper()
	publishRaw(t, url, bus.CommentCreatedRoutingKey, amqp.Publishing{Headers: amqp.Table{}, ContentType: bus.JSONContentType, DeliveryMode: amqp.Persistent, MessageId: "invalid-message", Timestamp: time.Now().UTC(), Type: string(bus.CommentCreated), Body: []byte(`{"not":"an envelope"}`)})
}
func publishRaw(t *testing.T, url, routing string, message amqp.Publishing) {
	t.Helper()
	connection, err := amqp.Dial(url)
	if err != nil {
		t.Fatalf("dial RabbitMQ: %v", err)
	}
	defer connection.Close()
	channel, err := connection.Channel()
	if err != nil {
		t.Fatalf("open RabbitMQ channel: %v", err)
	}
	defer channel.Close()
	if err := platform.DeclareBusinessTopology(channel, time.Second); err != nil {
		t.Fatalf("declare topology: %v", err)
	}
	if err := channel.Confirm(false); err != nil {
		t.Fatalf("enable confirms: %v", err)
	}
	confirms := channel.NotifyPublish(make(chan amqp.Confirmation, 1))
	if err := channel.PublishWithContext(context.Background(), platform.BusinessExchange, routing, true, false, message); err != nil {
		t.Fatalf("publish: %v", err)
	}
	select {
	case confirm := <-confirms:
		if !confirm.Ack {
			t.Fatal("publish was nacked")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("publish confirm timed out")
	}
}
func purgeBusinessQueues(t *testing.T, url string) {
	t.Helper()
	connection, err := amqp.Dial(url)
	if err != nil {
		t.Fatalf("dial RabbitMQ: %v", err)
	}
	defer connection.Close()
	channel, _ := connection.Channel()
	defer channel.Close()
	if err := platform.DeclareBusinessTopology(channel, time.Second); err != nil {
		t.Fatalf("declare topology: %v", err)
	}
	for _, queue := range []string{platform.BusinessQueue, platform.BusinessRetryQueue, platform.BusinessDeadQueue} {
		if _, err := channel.QueuePurge(queue, false); err != nil {
			t.Fatalf("purge %s: %v", queue, err)
		}
	}
}
func queueMessages(t *testing.T, url, queue string) int {
	t.Helper()
	connection, err := amqp.Dial(url)
	if err != nil {
		t.Fatalf("dial RabbitMQ: %v", err)
	}
	defer connection.Close()
	channel, _ := connection.Channel()
	defer channel.Close()
	result, err := channel.QueueInspect(queue)
	if err != nil {
		t.Fatalf("inspect queue: %v", err)
	}
	return result.Messages
}
func waitQueueMessages(t *testing.T, ctx context.Context, url, queue string, want int) {
	t.Helper()
	for {
		if queueMessages(t, url, queue) >= want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("queue %s did not reach %d messages", queue, want)
		case <-time.After(100 * time.Millisecond):
		}
	}
}
func waitNotificationCount(t *testing.T, ctx context.Context, db *sql.DB, postID uint64, want int) {
	t.Helper()
	for {
		var count int
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE post_id = ?`, postID).Scan(&count)
		if count >= want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("notification count=%d want=%d", count, want)
		case <-time.After(100 * time.Millisecond):
		}
	}
}
func waitEventNotification(t *testing.T, ctx context.Context, db *sql.DB, eventID string) {
	t.Helper()
	for {
		var count int
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE source_event_id = ?`, eventID).Scan(&count)
		if count == 1 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("event %s notification missing", eventID)
		case <-time.After(100 * time.Millisecond):
		}
	}
}
func assertEventNotificationCount(t *testing.T, ctx context.Context, db *sql.DB, eventID string, want int) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE source_event_id = ?`, eventID).Scan(&count); err != nil || count != want {
		t.Fatalf("event notification count=%d error=%v want=%d", count, err, want)
	}
}
func workerInsertUser(t *testing.T, ctx context.Context, db *sql.DB, prefix string) uint64 {
	t.Helper()
	result, err := db.ExecContext(ctx, `INSERT INTO users (username,password_hash) VALUES (?,?)`, fmt.Sprintf("%.12s_%012d", prefix, time.Now().UnixNano()%1_000_000_000_000), "$2a$10$integration-placeholder")
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	id, _ := result.LastInsertId()
	return uint64(id)
}
func workerInsertPost(t *testing.T, ctx context.Context, db *sql.DB, author uint64) uint64 {
	t.Helper()
	result, err := db.ExecContext(ctx, `INSERT INTO posts (author_id,title,content) VALUES (?,'worker integration','content')`, author)
	if err != nil {
		t.Fatalf("insert post: %v", err)
	}
	id, _ := result.LastInsertId()
	return uint64(id)
}
func workerInsertComment(t *testing.T, ctx context.Context, db *sql.DB, post, author uint64) uint64 {
	t.Helper()
	result, err := db.ExecContext(ctx, `INSERT INTO comments (post_id,author_id,content) VALUES (?,?,'worker integration')`, post, author)
	if err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	id, _ := result.LastInsertId()
	return uint64(id)
}
func workerCleanup(t *testing.T, db *sql.DB, post, actor, recipient uint64) {
	t.Helper()
	var once sync.Once
	once.Do(func() {
		ctx := context.Background()
		for _, q := range []string{`DELETE FROM notifications WHERE post_id = ?`, `DELETE FROM comments WHERE post_id = ?`, `DELETE FROM post_likes WHERE post_id = ?`, `DELETE FROM posts WHERE id = ?`} {
			if _, err := db.ExecContext(ctx, q, post); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM users WHERE id IN (?,?)`, actor, recipient); err != nil {
			t.Errorf("cleanup users: %v", err)
		}
	})
}
