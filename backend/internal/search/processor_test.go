package search

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/worker"
)

type performerFunc func(context.Context, *http.Request) (*http.Response, error)

func (performer performerFunc) Perform(ctx context.Context, request *http.Request) (*http.Response, error) {
	return performer(ctx, request)
}

type processorStore struct {
	document Document
	err      error
}

func (store processorStore) FindDocument(context.Context, uint64) (Document, error) {
	return store.document, store.err
}

type processorIndexer struct {
	document Document
	err      error
}

func (indexer *processorIndexer) IndexAlias(_ context.Context, document Document) error {
	indexer.document = document
	return indexer.err
}

func TestProcessorIndexesPostCreatedFromStore(t *testing.T) {
	document := Document{PostID: 7, Title: "indexed", Content: "from mysql", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	indexer := &processorIndexer{}
	processor, err := NewProcessor(processorStore{document: document}, indexer)
	if err != nil {
		t.Fatal(err)
	}
	event, err := bus.NewPostCreated(time.Now().UTC(), 3, 7)
	if err != nil {
		t.Fatal(err)
	}
	if err := processor.Process(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if indexer.document.PostID != document.PostID {
		t.Fatalf("indexed document = %#v", indexer.document)
	}
}

func TestProcessorClassifiesPermanentFailures(t *testing.T) {
	event, err := bus.NewPostCreated(time.Now().UTC(), 3, 7)
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		store   processorStore
		indexer *processorIndexer
	}{
		"mapping rejection": {store: processorStore{document: Document{PostID: 7, Title: "x", Content: "y", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}}, indexer: &processorIndexer{err: &PermanentIndexError{Reason: "index_mapping_rejected"}}},
	} {
		t.Run(name, func(t *testing.T) {
			processor, _ := NewProcessor(test.store, test.indexer)
			if err := processor.Process(context.Background(), event); !worker.IsPermanent(err) {
				t.Fatalf("Process() error = %v", err)
			}
		})
	}
	processor, _ := NewProcessor(processorStore{err: errors.New("mysql unavailable")}, &processorIndexer{})
	if err := processor.Process(context.Background(), event); err == nil || worker.IsPermanent(err) {
		t.Fatalf("temporary Process() error = %v", err)
	}
}

func TestElasticsearchRepositoryIndexAliasRequiresAliasAndClassifiesStatuses(t *testing.T) {
	document := Document{PostID: 7, Title: "indexed", Content: "from mysql", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	for _, test := range []struct {
		name      string
		status    int
		permanent bool
		temporary bool
	}{
		{name: "success", status: http.StatusCreated},
		{name: "mapping rejection", status: http.StatusBadRequest, permanent: true},
		{name: "missing alias", status: http.StatusNotFound, temporary: true},
		{name: "rate limited", status: http.StatusTooManyRequests, temporary: true},
		{name: "server unavailable", status: http.StatusServiceUnavailable, temporary: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requestPath, requestQuery string
			repository := NewElasticsearchRepository(performerFunc(func(_ context.Context, request *http.Request) (*http.Response, error) {
				requestPath, requestQuery = request.URL.Path, request.URL.RawQuery
				if request.Method != http.MethodPut {
					t.Fatalf("method = %s", request.Method)
				}
				body, err := io.ReadAll(request.Body)
				if err != nil || !strings.Contains(string(body), `"post_id":7`) || strings.Contains(string(body), "recipient_id") {
					t.Fatalf("request body = %q, %v", string(body), err)
				}
				return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			}))
			err := repository.IndexAlias(context.Background(), document)
			if requestPath != "/"+AliasName+"/_doc/7" || requestQuery != "require_alias=true" {
				t.Fatalf("request = %s?%s", requestPath, requestQuery)
			}
			var permanent *PermanentIndexError
			switch {
			case test.permanent && !errors.As(err, &permanent):
				t.Fatalf("IndexAlias() error = %v, want permanent", err)
			case test.temporary && !errors.Is(err, ErrUnavailable):
				t.Fatalf("IndexAlias() error = %v, want temporary unavailable", err)
			case !test.permanent && !test.temporary && err != nil:
				t.Fatalf("IndexAlias() error = %v", err)
			}
		})
	}
}

func TestProcessorReplaysUpdateAndCreateFromCurrentFact(t *testing.T) {
	current := Document{PostID: 7, ContentRevision: 3, Title: "latest", Content: "current", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	indexer := &processorIndexer{}
	processor, _ := NewProcessor(processorStore{document: current}, indexer)
	old, _ := bus.NewPostUpdated(time.Now().UTC(), 3, 7, 2)
	created, _ := bus.NewPostCreated(time.Now().UTC(), 3, 7)
	for _, event := range []bus.Envelope{old, created, old} {
		if err := processor.Process(context.Background(), event); err != nil {
			t.Fatal(err)
		}
		if indexer.document.ContentRevision != 3 || indexer.document.Title != "latest" {
			t.Fatal(indexer.document)
		}
	}
}
func TestIndexRevisionConflictIsSuccessfulStaleReplay(t *testing.T) {
	repo := NewElasticsearchRepository(performerFunc(func(_ context.Context, req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("version") != "2" || req.URL.Query().Get("version_type") != "external_gte" {
			t.Fatal(req.URL)
		}
		return &http.Response{StatusCode: 409, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	}))
	if err := repo.IndexAlias(context.Background(), Document{PostID: 7, ContentRevision: 2, Title: "old", Content: "old", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
}
func TestProcessorDeletesMissingFact(t *testing.T) {
	indexer := &processorIndexer{}
	processor, _ := NewProcessor(processorStore{err: sql.ErrNoRows}, indexer)
	event, _ := bus.NewPostCreated(time.Now().UTC(), 3, 7)
	if err := processor.Process(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if indexer.document.PostID != 7 {
		t.Fatal("missing delete")
	}
}
func (i *processorIndexer) DeleteAlias(_ context.Context, id uint64) error {
	i.document = Document{PostID: id}
	return i.err
}

func TestDeletedThenOldCreateUpdateNeverRebuildsPayload(t *testing.T) {
	indexer := &processorIndexer{}
	processor, _ := NewProcessor(processorStore{err: sql.ErrNoRows}, indexer)
	deleted, _ := bus.NewPostDeleted(time.Now().UTC(), 3, 7)
	created, _ := bus.NewPostCreated(time.Now().UTC(), 3, 7)
	updated, _ := bus.NewPostUpdated(time.Now().UTC(), 3, 7, 2)
	for _, event := range []bus.Envelope{deleted, created, updated, deleted} {
		if err := processor.Process(context.Background(), event); err != nil {
			t.Fatal(err)
		}
		if indexer.document.Title != "" || indexer.document.Content != "" {
			t.Fatal("deleted content resurrected")
		}
	}
}
