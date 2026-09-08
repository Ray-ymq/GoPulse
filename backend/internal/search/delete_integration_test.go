//go:build integration

package search

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
)

func TestIntegrationDeletedPostOldEventsConvergeWithoutReindex(t *testing.T) {
	cfg := integrationtest.Environment(t)
	ctx := context.Background()
	db, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	release := integrationtest.AcquirePostFactsLock(t, db)
	defer release()
	actor, err := user.NewMySQLRepository(db).Create(ctx, "sd_"+time.Now().Format("150405000000"), "hash")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DELETE FROM users WHERE id=?", actor.ID)
	events, _ := outbox.NewRepository(db, outbox.Options{})
	posts := post.NewMySQLRepositoryWithOutbox(db, events)
	record, err := posts.Create(ctx, actor.ID, "deletedsearchword", "deleted body")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DELETE FROM posts WHERE id=?", record.ID)
	defer db.Exec("DELETE FROM business_outbox WHERE JSON_EXTRACT(payload,'$.post_id')=?", record.ID)
	client, err := platform.NewElasticsearch(cfg.Elasticsearch)
	if err != nil {
		t.Fatal(err)
	}
	index := NewElasticsearchRepository(client)
	// Existing integration setup owns the alias; no manual projection repair.
	if _, err := NewReindexer(NewReindexStore(db), index, 10).Run(ctx, true); err != nil {
		t.Fatal(err)
	}
	processor, _ := NewProcessor(NewMySQLDocumentStore(db), index)
	created, _ := bus.NewPostCreated(time.Now().UTC(), actor.ID, record.ID)
	updated, _ := bus.NewPostUpdated(time.Now().UTC(), actor.ID, record.ID, 2)
	deleted, _ := bus.NewPostDeleted(time.Now().UTC(), actor.ID, record.ID)
	if err := processor.Process(ctx, created); err != nil {
		t.Fatal(err)
	}
	if err := posts.Delete(ctx, record.ID, actor.ID); err != nil {
		t.Fatal(err)
	}
	for _, event := range []bus.Envelope{deleted, created, updated, deleted} {
		if err := processor.Process(ctx, event); err != nil {
			t.Fatal(err)
		}
		response, err := index.do(ctx, http.MethodGet, fmt.Sprintf("/%s/_doc/%d", AliasName, record.ID), nil)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("deleted projection status=%d", response.StatusCode)
		}
	}
}
