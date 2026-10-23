package search

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

const advisoryLockName = "gopulse:post-search-reindex:v1"

type reindexDatabase interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type ReindexStore struct {
	database *sql.DB
	reader   reindexDatabase
}

func (store *ReindexStore) queryer() reindexDatabase {
	if store.reader != nil {
		return store.reader
	}
	return store.database
}

func NewReindexStore(database *sql.DB) *ReindexStore { return &ReindexStore{database: database} }

type Lock struct{ connection *sql.Conn }

func (store *ReindexStore) Acquire(ctx context.Context) (*Lock, error) {
	if store == nil || store.database == nil {
		return nil, errors.New("reindex database is unavailable")
	}
	connection, err := store.database.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire reindex connection: %w", err)
	}
	var acquired sql.NullInt64
	if err := connection.QueryRowContext(ctx, `SELECT GET_LOCK(?, 0)`, advisoryLockName).Scan(&acquired); err != nil || !acquired.Valid || acquired.Int64 != 1 {
		_ = connection.Close()
		if err != nil {
			return nil, fmt.Errorf("acquire reindex lock: %w", err)
		}
		return nil, errors.New("another search reindex is running")
	}
	return &Lock{connection: connection}, nil
}

func (lock *Lock) Release(ctx context.Context) error {
	if lock == nil || lock.connection == nil {
		return nil
	}
	defer func() { _ = lock.connection.Close() }()
	var released sql.NullInt64
	if err := lock.connection.QueryRowContext(ctx, `SELECT RELEASE_LOCK(?)`, advisoryLockName).Scan(&released); err != nil {
		return fmt.Errorf("release reindex lock: %w", err)
	}
	if !released.Valid || released.Int64 != 1 {
		return errors.New("reindex lock was not owned")
	}
	return nil
}

func (store *ReindexStore) HighWatermark(ctx context.Context) (uint64, error) {
	var maximum sql.NullInt64
	if err := store.queryer().QueryRowContext(ctx, `SELECT GREATEST(
 COALESCE((SELECT MAX(id) FROM posts), 0),
 COALESCE((SELECT MAX(t.post_id) FROM post_search_tombstones t
  JOIN business_environment e ON e.singleton=1 AND e.data_epoch=t.data_epoch), 0)
)`).Scan(&maximum); err != nil {
		return 0, fmt.Errorf("read post high watermark: %w", err)
	}
	if !maximum.Valid {
		return 0, nil
	}
	return uint64(maximum.Int64), nil
}

func (store *ReindexStore) CountUpTo(ctx context.Context, maximum uint64) (uint64, error) {
	var count uint64
	if err := store.queryer().QueryRowContext(ctx, `SELECT COUNT(*) FROM posts WHERE id <= ?`, maximum).Scan(&count); err != nil {
		return 0, fmt.Errorf("count posts for reindex: %w", err)
	}
	return count, nil
}

func (store *ReindexStore) Scan(ctx context.Context, after, maximum uint64, limit int) ([]Document, error) {
	rows, err := store.queryer().QueryContext(ctx, `
SELECT id, title, content, created_at, updated_at, edited_at, content_revision, deleted FROM (
 (SELECT id, title, content, created_at, updated_at, edited_at, content_revision, FALSE AS deleted
  FROM posts WHERE id > ? AND id <= ? ORDER BY id ASC LIMIT ?)
 UNION ALL
 (SELECT t.post_id, '', '', t.deleted_at, t.deleted_at, NULL, t.content_revision, TRUE
  FROM post_search_tombstones t JOIN business_environment e ON e.singleton=1 AND e.data_epoch=t.data_epoch
  WHERE t.post_id > ? AND t.post_id <= ? ORDER BY t.post_id ASC LIMIT ?)
) AS facts ORDER BY id ASC LIMIT ?`, after, maximum, limit, after, maximum, limit, limit)
	if err != nil {
		return nil, fmt.Errorf("scan posts for reindex: %w", err)
	}
	defer func() { _ = rows.Close() }()
	documents := make([]Document, 0, limit)
	for rows.Next() {
		var document Document
		if err := rows.Scan(&document.PostID, &document.Title, &document.Content, &document.CreatedAt, &document.UpdatedAt, &document.EditedAt, &document.ContentRevision, &document.Deleted); err != nil {
			return nil, fmt.Errorf("scan posts for reindex: %w", err)
		}
		documents = append(documents, document)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan posts for reindex: %w", err)
	}
	return documents, nil
}

type ReindexClient interface {
	AliasExists(context.Context) (bool, error)
	CreateIndex(context.Context, string) error
	BulkIndex(context.Context, string, []Document) error
	Refresh(context.Context, string) error
	Count(context.Context, string) (uint64, error)
	SwitchAlias(context.Context, string) ([]string, error)
	DeleteIndex(context.Context, string) error
}

type ReindexResult struct {
	Changed       bool
	DocumentCount uint64
	Generation    string
}

type Reindexer struct {
	store  *ReindexStore
	client ReindexClient
	batch  int
	epoch  string
}

func NewReindexer(store *ReindexStore, client ReindexClient, batch int) *Reindexer {
	return &Reindexer{store: store, client: client, batch: batch}
}

func (reindexer *Reindexer) Run(ctx context.Context, ifMissing bool) (ReindexResult, error) {
	return reindexer.RunForEpoch(ctx, ifMissing, "")
}

// RunForEpoch builds a fresh physical generation carrying the supplied data
// epoch. It never inserts application-completion rows: a rebuild is a fact
// scan, not the replay of any individual event.
func (reindexer *Reindexer) RunForEpoch(ctx context.Context, ifMissing bool, epoch string) (ReindexResult, error) {
	if reindexer == nil || reindexer.store == nil || reindexer.client == nil || reindexer.batch < 1 {
		return ReindexResult{}, errors.New("search reindexer is invalid")
	}
	if epoch != "" && !validEpoch(epoch) {
		return ReindexResult{}, errors.New("search reindex epoch is invalid")
	}
	reindexer.epoch = epoch
	lock, err := reindexer.store.Acquire(ctx)
	if err != nil {
		return ReindexResult{}, err
	}
	defer func() { _ = lock.Release(context.Background()) }()

	local := *reindexer
	local.store = &ReindexStore{database: reindexer.store.database, reader: lock.connection}
	reindexer = &local

	if ifMissing {
		exists, err := reindexer.client.AliasExists(ctx)
		if err != nil {
			return ReindexResult{}, fmt.Errorf("check search alias: %w", err)
		}
		if exists {
			// Legacy indices used internal ES versions, which can exceed a
			// post's first content revision. Rebuild once rather than rejecting
			// valid edits as stale forever after an in-place mapping update.
			if checker, ok := reindexer.client.(interface {
				RevisionMappingReady(context.Context) (bool, error)
			}); ok {
				ready, err := checker.RevisionMappingReady(ctx)
				if err != nil {
					return ReindexResult{}, err
				}
				if ready && epoch == "" {
					return ReindexResult{Changed: false}, nil
				}
			} else if epoch == "" {
				return ReindexResult{Changed: false}, nil
			}
			if epoch != "" {
				if metadata, ok := reindexer.client.(interface {
					IndexEpoch(context.Context, string) (string, error)
				}); ok {
					current, metadataErr := reindexer.currentAliasGeneration(ctx)
					if metadataErr == nil {
						actual, epochErr := metadata.IndexEpoch(ctx, current)
						if epochErr == nil && actual == epoch && readyMapping(reindexer.client, ctx) {
							return ReindexResult{Changed: false, Generation: current}, nil
						}
					}
				}
			}
		}
	}

	index, err := newPhysicalIndexName()
	if err != nil {
		return ReindexResult{}, err
	}
	if creator, ok := reindexer.client.(interface {
		CreateIndexForEpoch(context.Context, string, string) error
	}); ok {
		err = creator.CreateIndexForEpoch(ctx, index, epoch)
	} else if epoch == "" {
		err = reindexer.client.CreateIndex(ctx, index)
	} else {
		err = errors.New("search client cannot create an epoch-bound index")
	}
	if err != nil {
		return ReindexResult{}, fmt.Errorf("create search index: %w", err)
	}
	switched := false
	defer func() {
		if !switched {
			_ = reindexer.client.DeleteIndex(context.Background(), index)
		}
	}()

	h1, err := reindexer.store.HighWatermark(ctx)
	if err != nil {
		return ReindexResult{}, err
	}
	if err := reindexer.copyRange(ctx, index, 0, h1); err != nil {
		return ReindexResult{}, err
	}
	if err := reindexer.client.Refresh(ctx, index); err != nil {
		return ReindexResult{}, fmt.Errorf("refresh search index before alias switch: %w", err)
	}
	mysqlCount, err := reindexer.store.CountUpTo(ctx, h1)
	if err != nil {
		return ReindexResult{}, err
	}
	indexCount, err := reindexer.client.Count(ctx, index)
	if err != nil {
		return ReindexResult{}, fmt.Errorf("count search index before alias switch: %w", err)
	}
	if mysqlCount != indexCount {
		return ReindexResult{}, fmt.Errorf("search index count mismatch before alias switch: mysql=%d index=%d", mysqlCount, indexCount)
	}
	oldIndices, err := reindexer.client.SwitchAlias(ctx, index)
	if err != nil {
		return ReindexResult{}, fmt.Errorf("switch search alias: %w", err)
	}
	switched = true

	h2, err := reindexer.store.HighWatermark(ctx)
	if err != nil {
		return ReindexResult{}, err
	}
	if h2 > 0 {
		if err := reindexer.copyRange(ctx, index, 0, h2); err != nil {
			return ReindexResult{}, err
		}
	}
	if err := reindexer.client.Refresh(ctx, index); err != nil {
		return ReindexResult{}, fmt.Errorf("refresh search index after tail compensation: %w", err)
	}
	mysqlCount, err = reindexer.store.CountUpTo(ctx, h2)
	if err != nil {
		return ReindexResult{}, err
	}
	indexCount, err = reindexer.client.Count(ctx, index)
	if err != nil {
		return ReindexResult{}, fmt.Errorf("count search index after tail compensation: %w", err)
	}
	if mysqlCount != indexCount {
		return ReindexResult{}, fmt.Errorf("search index count mismatch after tail compensation: mysql=%d index=%d", mysqlCount, indexCount)
	}
	for _, oldIndex := range oldIndices {
		if oldIndex == index {
			continue
		}
		if err := reindexer.client.DeleteIndex(ctx, oldIndex); err != nil {
			return ReindexResult{}, fmt.Errorf("delete old search index: %w", err)
		}
	}
	if epoch != "" {
		if actual, err := reindexer.currentAliasGeneration(ctx); err != nil || actual != index {
			return ReindexResult{}, errors.New("search alias generation changed during epoch reindex")
		}
		metadata, ok := reindexer.client.(interface {
			IndexEpoch(context.Context, string) (string, error)
		})
		if !ok {
			return ReindexResult{}, errors.New("search client cannot verify index epoch metadata")
		}
		actual, err := metadata.IndexEpoch(ctx, index)
		if err != nil || actual != epoch {
			return ReindexResult{}, errors.New("search index epoch metadata mismatch")
		}
	}
	return ReindexResult{Changed: true, DocumentCount: mysqlCount, Generation: index}, nil
}

func (reindexer *Reindexer) copyRange(ctx context.Context, index string, after, maximum uint64) error {
	for after < maximum {
		documents, err := reindexer.store.Scan(ctx, after, maximum, reindexer.batch)
		if err != nil {
			return err
		}
		if len(documents) == 0 {
			break
		}
		for index := range documents {
			documents[index].DataEpoch = reindexer.epoch
		}
		if err := reindexer.client.BulkIndex(ctx, index, documents); err != nil {
			return fmt.Errorf("bulk index posts: %w", err)
		}
		after = documents[len(documents)-1].PostID
	}
	return nil
}

func (reindexer *Reindexer) currentAliasGeneration(ctx context.Context) (string, error) {
	if resolver, ok := reindexer.client.(interface {
		ResolveGeneration(context.Context) (string, error)
	}); ok {
		return resolver.ResolveGeneration(ctx)
	}
	return "", ErrUnavailable
}

func readyMapping(client ReindexClient, ctx context.Context) bool {
	checker, ok := client.(interface {
		RevisionMappingReady(context.Context) (bool, error)
	})
	if !ok {
		return true
	}
	ready, err := checker.RevisionMappingReady(ctx)
	return err == nil && ready
}

func newPhysicalIndexName() (string, error) {
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate physical index name: %w", err)
	}
	return PhysicalIndexPrefix + time.Now().UTC().Format("20060102t150405000000000z") + "-" + hex.EncodeToString(random), nil
}
