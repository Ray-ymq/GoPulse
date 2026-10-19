//go:build integration

package outbox_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
)

func TestIntegrationClaimFairnessBatchLeaseResultsAndCancellation(t *testing.T) {
	cfg := integrationtest.Environment(t)
	db, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	repository, err := outbox.NewRepository(db, outbox.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	defer func() {
		for _, id := range ids {
			_, _ = db.Exec(`DELETE FROM business_outbox WHERE event_id=?`, id)
		}
	}()
	for i := 0; i < 3; i++ {
		event := newIntegrationEvent(t, time.Now().UTC(), uint64(800+i))
		ids = append(ids, event.EventID)
		if err := repository.Insert(ctx, db, event); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE business_outbox SET status='leased',lease_owner='expired',lease_expires_at=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE event_id=?`, ids[2]); err != nil {
		t.Fatal(err)
	}
	first, err := repository.Claim(ctx, "fair", 1, time.Minute)
	if err != nil || len(first) != 1 || first[0].EventID == ids[2] {
		t.Fatalf("first branch pending: %v %v", first, err)
	}
	second, err := repository.Claim(ctx, "fair", 1, time.Minute)
	if err != nil || len(second) != 1 || second[0].EventID != ids[2] {
		t.Fatalf("expired branch starved: %v %v", second, err)
	}
	if _, err := db.Exec(`UPDATE business_outbox SET lease_owner='another' WHERE id=?`, second[0].ID); err != nil {
		t.Fatal(err)
	}
	results, err := repository.MarkPublishedBatch(ctx, []uint64{first[0].ID, second[0].ID}, "fair")
	if err != nil || len(results) != 2 || results[0] != nil || !errors.Is(results[1], outbox.ErrLeaseLost) {
		t.Fatalf("batch lease results: %v %v", results, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := repository.Claim(canceled, "fair", 1, time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestIntegrationOutbox550kHistoryUsesIndexesAndBoundedCleanup(t *testing.T) {
	cfg := integrationtest.Environment(t)
	db, err := platform.OpenMySQLMigrationDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	marker := fmt.Sprintf("history-%d", time.Now().UnixNano())
	defer func() {
		_, _ = db.Exec(`DELETE r FROM business_event_requirements r JOIN business_outbox o ON o.event_id=r.event_id WHERE o.last_error=?`, marker)
		_, _ = db.Exec(`DELETE FROM business_outbox WHERE last_error=?`, marker)
	}()
	_, err = db.ExecContext(ctx, `INSERT INTO business_outbox (event_id,event_type,schema_version,payload,status,available_at,published_at,last_error,created_at,updated_at)
 WITH RECURSIVE n AS (SELECT 0 AS i UNION ALL SELECT i+1 FROM n WHERE i<999)
 SELECT UUID(),'post.liked',1,JSON_OBJECT(),'published',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)-INTERVAL 8 DAY,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)
 FROM n a JOIN n b ON b.i<550`, marker)
	if err != nil {
		t.Fatal(err)
	}
	queries := []struct {
		query, index string
		args         []any
	}{
		{`SELECT id,payload FROM business_outbox WHERE status='pending' AND available_at<=? ORDER BY available_at,id LIMIT 50 FOR UPDATE SKIP LOCKED`, "idx_business_outbox_pending", []any{time.Now().UTC()}},
		{`SELECT id,payload FROM business_outbox WHERE status='leased' AND lease_expires_at<=? ORDER BY lease_expires_at,id LIMIT 50 FOR UPDATE SKIP LOCKED`, "idx_business_outbox_lease_recovery", []any{time.Now().UTC()}},
		{`SELECT id,published_at FROM business_outbox WHERE status='published' AND published_at<? AND (published_at>? OR (published_at=? AND id>?)) ORDER BY published_at,id LIMIT 500`, "idx_business_outbox_published_cleanup", []any{time.Now().UTC(), time.Time{}, time.Time{}, 0}},
	}
	for _, q := range queries {
		var plan string
		if err := db.QueryRowContext(ctx, "EXPLAIN FORMAT=JSON "+q.query, q.args...).Scan(&plan); err != nil {
			t.Fatal(err)
		}
		var decoded any
		if err := json.Unmarshal([]byte(plan), &decoded); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(plan, `"key": "`+q.index+`"`) {
			t.Fatalf("expected %s: %s", q.index, plan)
		}
		t.Logf("550k history query uses %s", q.index)
	}
	// Protect the first entire candidate page; the next page must remain reachable.
	_, err = db.ExecContext(ctx, `INSERT INTO business_event_requirements (data_epoch,event_id,consumer,routing_contract_revision,applicable_object_version,payload_bytes,created_at,updated_at)
 SELECT e.data_epoch,o.event_id,'search','post-created-search/1',1,1,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)
 FROM business_outbox o JOIN business_environment e ON e.singleton=1 WHERE o.last_error=? ORDER BY o.published_at,o.id LIMIT 500`, marker)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := outbox.NewRepository(db, outbox.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for page := 0; page < 2; page++ {
		op, cancel := context.WithTimeout(ctx, 2*time.Second)
		deleted, err := repository.CleanupPublished(op, time.Now().UTC().Add(-7*24*time.Hour), 500)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		want := int64(0)
		if page == 1 {
			want = 500
		}
		if deleted != want || !repository.CleanupHasMore() {
			t.Fatalf("page=%d deleted=%d more=%t", page, deleted, repository.CleanupHasMore())
		}
	}
}
