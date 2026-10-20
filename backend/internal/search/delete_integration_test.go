//go:build integration

package search

import (
	"context"
	"encoding/json"
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
		var stored struct {
			Source Document `json:"_source"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&stored)
		response.Body.Close()
		if response.StatusCode != http.StatusOK || decodeErr != nil || !stored.Source.Deleted || stored.Source.ContentRevision != 2 {
			t.Fatalf("deleted projection status=%d", response.StatusCode)
		}
	}
}

type blockedIndexer struct {
	*ElasticsearchRepository
	entered, release chan struct{}
}

func (b *blockedIndexer) IndexAlias(ctx context.Context, d Document) error {
	close(b.entered)
	select {
	case <-b.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return b.ElasticsearchRepository.IndexAlias(ctx, d)
}

// Implement only IndexAlias so the processor cannot bypass the test barrier.
type blockingIndexerOnly struct{ blocked *blockedIndexer }

func (b blockingIndexerOnly) IndexAlias(ctx context.Context, d Document) error {
	return b.blocked.IndexAlias(ctx, d)
}

func TestIntegrationSearchRemoteWriteDoesNotLockPost(t *testing.T) {
	cfg := integrationtest.Environment(t)
	db, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	release := integrationtest.AcquirePostFactsLock(t, db)
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	actor, err := user.NewMySQLRepository(db).Create(ctx, "blocked_"+time.Now().Format("150405000000"), "hash")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DELETE FROM users WHERE id=?", actor.ID)
	events, _ := outbox.NewRepository(db, outbox.Options{})
	posts := post.NewMySQLRepositoryWithOutbox(db, events)
	record, err := posts.Create(ctx, actor.ID, "blockedword", "body")
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
	barrier := &blockedIndexer{ElasticsearchRepository: index, entered: make(chan struct{}), release: make(chan struct{})}
	processor, _ := NewProcessor(NewMySQLDocumentStore(db), blockingIndexerOnly{barrier})
	event, _ := bus.NewPostCreated(time.Now().UTC(), actor.ID, record.ID)
	done := make(chan error, 1)
	go func() { done <- processor.Process(ctx, event) }()
	select {
	case <-barrier.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// A one-connection pool also detects hidden second-connection acquisition.
	db.SetMaxOpenConns(1)
	editCtx, editCancel := context.WithTimeout(ctx, 2*time.Second)
	_, err = posts.Update(editCtx, record.ID, actor.ID, post.CreateInput{Title: "editedword", Content: "body 2"})
	editCancel()
	if err != nil {
		t.Fatal(err)
	}
	if err := posts.Delete(ctx, record.ID, actor.ID); err != nil {
		t.Fatal(err)
	}
	deleting, _ := NewProcessor(NewMySQLDocumentStore(db), index)
	if err := deleting.Process(ctx, event); err != nil {
		t.Fatal(err)
	}
	close(barrier.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	response, err := index.do(ctx, http.MethodGet, fmt.Sprintf("/%s/_doc/%d", AliasName, record.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var stored struct {
		Source Document `json:"_source"`
	}
	if err := json.NewDecoder(response.Body).Decode(&stored); err != nil || !stored.Source.Deleted || stored.Source.ContentRevision != 3 {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}
