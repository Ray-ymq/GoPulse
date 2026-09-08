//go:build integration

package search

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
)

type editDuringSwitch struct {
	*ElasticsearchRepository
	edit func()
}

func (c *editDuringSwitch) SwitchAlias(ctx context.Context, index string) ([]string, error) {
	c.edit()
	return c.ElasticsearchRepository.SwitchAlias(ctx, index)
}

func TestIntegrationEditReindexCompensationAndStaleReplay(t *testing.T) {
	cfg := integrationtest.Environment(t)
	ctx := context.Background()
	db, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	release := integrationtest.AcquirePostFactsLock(t, db)
	defer release()
	actor, err := user.NewMySQLRepository(db).Create(ctx, "se_"+time.Now().Format("150405000000"), "hash")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DELETE FROM users WHERE id=?", actor.ID)
	events, err := outbox.NewRepository(db, outbox.Options{})
	if err != nil {
		t.Fatal(err)
	}
	posts := post.NewMySQLRepositoryWithOutbox(db, events)
	record, err := posts.Create(ctx, actor.ID, "oldeditword", "oldeditword")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DELETE FROM posts WHERE id=?", record.ID)
	defer db.Exec("DELETE FROM business_outbox WHERE JSON_EXTRACT(payload, '$.post_id')=?", record.ID)
	client, err := platform.NewElasticsearch(cfg.Elasticsearch)
	if err != nil {
		t.Fatal(err)
	}
	index := NewElasticsearchRepository(client)
	store := NewMySQLDocumentStore(db)
	stale, err := store.FindDocument(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	processor, _ := NewProcessor(store, index)
	rebuild := NewReindexer(NewReindexStore(db), index, 10)
	legacy, err := newPhysicalIndexName()
	if err != nil {
		t.Fatal(err)
	}
	response, err := index.do(ctx, http.MethodPut, "/"+legacy, strings.NewReader(`{"mappings":{"properties":{"title":{"type":"text"}}}}`))
	if err := expectSuccess(response, err); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		response, err = index.do(ctx, http.MethodPut, fmt.Sprintf("/%s/_doc/%d", legacy, record.ID), strings.NewReader(`{"title":"oldeditword"}`))
		if err := expectSuccess(response, err); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := index.SwitchAlias(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	if result, err := rebuild.Run(ctx, true); err != nil || !result.Changed {
		t.Fatal(err)
	}
	defer func() {
		generation, err := index.ResolveGeneration(ctx)
		if err == nil {
			_ = index.DeleteIndex(ctx, generation)
		}
	}()
	// Update is consumed on the OLD alias after the new index's first scan.
	// The post-switch second scan must compensate this otherwise lost update.
	switching := &editDuringSwitch{ElasticsearchRepository: index, edit: func() {
		if err := posts.Update(ctx, record.ID, actor.ID, post.CreateInput{Title: "neweditword", Content: "neweditword"}); err != nil {
			t.Fatal(err)
		}
		event, _ := bus.NewPostUpdated(time.Now().UTC(), actor.ID, record.ID, 2)
		if err := processor.Process(ctx, event); err != nil {
			t.Fatal(err)
		}
	}}
	if _, err := NewReindexer(NewReindexStore(db), switching, 10).Run(ctx, false); err != nil {
		t.Fatal(err)
	}
	generation, err := index.ResolveGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := index.IndexAlias(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if err := index.BulkIndex(ctx, generation, []Document{stale}); err != nil {
		t.Fatal(err)
	}
	created, _ := bus.NewPostCreated(time.Now().UTC(), actor.ID, record.ID)
	updated, _ := bus.NewPostUpdated(time.Now().UTC(), actor.ID, record.ID, 2)
	for _, event := range []bus.Envelope{created, updated, created} {
		if err := processor.Process(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	if err := index.Refresh(ctx, generation); err != nil {
		t.Fatal(err)
	}
	pit, err := index.OpenPointInTime(ctx, generation)
	if err != nil {
		t.Fatal(err)
	}
	defer index.ClosePointInTime(ctx, pit)
	for term, want := range map[string]int{"oldeditword": 0, "neweditword": 1} {
		result, err := index.Search(ctx, generation, pit, term, 10, nil)
		if err != nil || len(result.Hits) != want {
			t.Fatalf("%s hits=%d err=%v", term, len(result.Hits), err)
		}
	}
}
