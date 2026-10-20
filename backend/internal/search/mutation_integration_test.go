//go:build integration

package search

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/businessstate"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
	"github.com/Ray-ymq/GoPulse/backend/internal/worker"
)

func TestIntegrationV2CreateEditDeleteSearchAndDuplicate(t *testing.T) {
	cfg := integrationtest.Environment(t)
	db, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	release := integrationtest.AcquirePostFactsLock(t, db)
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	state, _ := businessstate.NewRepository(db)
	environment, err := state.Ensure(ctx)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := state.Budget(ctx, environment.DataEpoch)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.ActivateV2(ctx, "search-mutation-test"); err != nil {
		t.Fatal(err)
	}
	actor, err := user.NewMySQLRepository(db).Create(ctx, "v2search_"+time.Now().Format("150405000000"), "hash")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, table := range []string{"business_application_results", "business_event_failures", "business_event_requirements"} {
			_, _ = db.Exec("DELETE FROM "+table+" WHERE event_id IN(SELECT event_id FROM business_outbox WHERE JSON_EXTRACT(payload,'$.actor_id')=?)", actor.ID)
		}
		_, _ = db.Exec("DELETE FROM business_commands WHERE user_id=?", actor.ID)
		_, _ = db.Exec("DELETE FROM business_outbox WHERE JSON_EXTRACT(payload,'$.actor_id')=?", actor.ID)
		_, _ = db.Exec("DELETE FROM posts WHERE author_id=?", actor.ID)
		_, _ = db.Exec("DELETE FROM users WHERE id=?", actor.ID)
		_, _ = db.Exec("UPDATE business_async_budget SET open_requirements=?,retained_groups=?,retained_bytes=? WHERE data_epoch=?", baseline.OpenRequirements, baseline.RetainedGroups, baseline.RetainedBytes, environment.DataEpoch)
		_, _ = db.Exec("UPDATE business_environment SET mode=?,v1_compatibility_enabled=? WHERE singleton=1", environment.Mode, environment.V1Compatibility)
	}()
	client, err := platform.NewElasticsearch(cfg.Elasticsearch)
	if err != nil {
		t.Fatal(err)
	}
	index := NewElasticsearchRepository(client)
	result, err := NewReindexer(NewReindexStore(db), index, 10).RunForEpoch(ctx, false, environment.DataEpoch)
	if err != nil {
		t.Fatal(err)
	}
	events, _ := outbox.NewRepository(db, outbox.Options{EnableV2: true, RetentionAccounting: state})
	limited, _ := businessstate.NewRepository(db, businessstate.WithOpenRequirementsLimit(baseline.OpenRequirements+1))
	posts := post.NewMySQLRepository(db, post.RepositoryOptions{Outbox: events, BusinessState: limited})
	processor, _ := NewProcessor(NewMySQLDocumentStore(db), index)
	processor.WithBusinessState(state)
	record, err := posts.Create(ctx, actor.ID, "v2createdword", "body")
	if err != nil {
		t.Fatal(err)
	}
	// Requirement admission failure must roll back the post, event and task.
	if _, err := posts.Update(ctx, record.ID, actor.ID, post.CreateInput{Title: "mustrollback", Content: "changed"}); err == nil {
		t.Fatal("edit ignored full task budget")
	}
	current, err := posts.FindPublicByID(ctx, record.ID)
	if err != nil || current.Title != "v2createdword" || current.ContentRevision != 1 {
		t.Fatalf("rollback=%+v err=%v", current, err)
	}
	project := func(kind string) {
		t.Helper()
		var raw []byte
		if err := db.QueryRow("SELECT payload FROM business_outbox WHERE event_type=? AND JSON_EXTRACT(payload,'$.post_id')=?", kind, record.ID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		event, err := bus.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		decision, err := state.CheckEnvelopeForConsumer(ctx, event, businessstate.ConsumerSearch)
		if err != nil || decision != businessstate.AdmissionAccept {
			t.Fatalf("admission=%v err=%v", decision, err)
		}
		if err := processor.Process(ctx, event); err != nil {
			t.Fatal(err)
		}
		if err := processor.Process(ctx, event); !errors.Is(err, worker.ErrDuplicate) {
			t.Fatalf("duplicate=%v", err)
		}
		if err := index.Refresh(ctx, result.Generation); err != nil {
			t.Fatal(err)
		}
	}
	search := func(term string, want int) {
		t.Helper()
		pit, err := index.OpenPointInTime(ctx, result.Generation)
		if err != nil {
			t.Fatal(err)
		}
		defer index.ClosePointInTime(ctx, pit)
		found, err := index.Search(ctx, result.Generation, pit, term, 10, nil)
		if err != nil || len(found.Hits) != want {
			t.Fatalf("query=%s hits=%d want=%d err=%v", term, len(found.Hits), want, err)
		}
	}
	project("post.created")
	search("v2createdword", 1)
	stale, err := NewMySQLDocumentStore(db).FindDocument(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	stale.DataEpoch = environment.DataEpoch
	if _, err := posts.Update(ctx, record.ID, actor.ID, post.CreateInput{Title: "v2editedword", Content: "body 2"}); err != nil {
		t.Fatal(err)
	}
	if err := posts.Delete(ctx, record.ID, actor.ID); err == nil {
		t.Fatal("delete ignored full task budget")
	}
	project("post.updated")
	search("v2createdword", 0)
	search("v2editedword", 1)
	if err := posts.Delete(ctx, record.ID, actor.ID); err != nil {
		t.Fatal(err)
	}
	project("post.deleted")
	search("v2editedword", 0)
	if result, err := index.IndexAliasResult(ctx, stale); err != nil || !result.ConflictVerified {
		t.Fatalf("stale write=%+v err=%v", result, err)
	}
}
