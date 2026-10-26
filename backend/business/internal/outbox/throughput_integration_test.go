//go:build integration

package outbox_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
)

// The acceptance rate includes SQL claim, real mandatory RabbitMQ publishing,
// publisher confirms/returns and durable SQL acknowledgements.
func TestIntegrationRabbitMQDispatchAtLeast300PerSecond(t *testing.T) {
	cfg := integrationtest.Environment(t)
	cfg.MySQL.MaxOpenConns = 4
	cfg.MySQL.MaxIdleConns = 4
	db, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository, err := outbox.NewRepository(db, outbox.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	const count = 5000
	ids := make([]string, 0, count)
	defer func() {
		for _, id := range ids {
			_, _ = db.ExecContext(context.Background(), `DELETE FROM business_outbox WHERE event_id=?`, id)
		}
	}()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < count; i++ {
		event := newIntegrationEvent(t, time.Now().UTC(), uint64(1000000+i))
		if err := repository.Insert(ctx, tx, event); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, event.EventID)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	publishers := []outbox.Publisher{}
	for i := 0; i < 8; i++ {
		p, err := platform.NewRabbitMQPublisher(cfg.RabbitMQURL)
		if err != nil {
			t.Fatal(err)
		}
		defer p.Close()
		publishers = append(publishers, p)
	}
	dispatcher, err := outbox.NewDispatcher(repository, publishers[0], outbox.DispatcherOptions{Owner: "throughput-acceptance", Publishers: publishers, ClaimBatch: 50, LeaseDuration: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	for i := 0; i < count/50; i++ {
		if err := dispatcher.DispatchOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	elapsed := time.Since(started)
	var published int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM business_outbox WHERE status='published' AND event_id IN (`+strings.TrimRight(strings.Repeat("?,", count), ",")+`)`, eventArgs(ids)...).Scan(&published); err != nil {
		t.Fatal(err)
	}
	rate := float64(published) / elapsed.Seconds()
	t.Logf("real RabbitMQ dispatch: published=%d elapsed=%s rate=%.2f events/s concurrency=8 batch=50 database_pool=4", published, elapsed, rate)
	if published != count || rate < 300 {
		t.Fatalf("hard acceptance failed: published=%d/%d rate=%.2f < 300", published, count, rate)
	}
}

func eventArgs(ids []string) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}
