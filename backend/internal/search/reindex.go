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

type ReindexStore struct{ database *sql.DB }

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
	defer lock.connection.Close()
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
	if err := store.database.QueryRowContext(ctx, `SELECT MAX(id) FROM posts`).Scan(&maximum); err != nil {
		return 0, fmt.Errorf("read post high watermark: %w", err)
	}
	if !maximum.Valid {
		return 0, nil
	}
	return uint64(maximum.Int64), nil
}

func (store *ReindexStore) CountUpTo(ctx context.Context, maximum uint64) (uint64, error) {
	var count uint64
	if err := store.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM posts WHERE id <= ?`, maximum).Scan(&count); err != nil {
		return 0, fmt.Errorf("count posts for reindex: %w", err)
	}
	return count, nil
}

func (store *ReindexStore) Scan(ctx context.Context, after, maximum uint64, limit int) ([]Document, error) {
	rows, err := store.database.QueryContext(ctx, `
SELECT id, title, content, created_at, updated_at
FROM posts
WHERE id > ? AND id <= ?
ORDER BY id ASC
LIMIT ?`, after, maximum, limit)
	if err != nil {
		return nil, fmt.Errorf("scan posts for reindex: %w", err)
	}
	defer rows.Close()
	documents := make([]Document, 0, limit)
	for rows.Next() {
		var document Document
		if err := rows.Scan(&document.PostID, &document.Title, &document.Content, &document.CreatedAt, &document.UpdatedAt); err != nil {
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

type Reindexer struct {
	store  *ReindexStore
	client ReindexClient
	batch  int
}

func NewReindexer(store *ReindexStore, client ReindexClient, batch int) *Reindexer {
	return &Reindexer{store: store, client: client, batch: batch}
}

func (reindexer *Reindexer) Run(ctx context.Context, ifMissing bool) error {
	if reindexer == nil || reindexer.store == nil || reindexer.client == nil || reindexer.batch < 1 {
		return errors.New("search reindexer is invalid")
	}
	lock, err := reindexer.store.Acquire(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release(context.Background()) }()

	if ifMissing {
		exists, err := reindexer.client.AliasExists(ctx)
		if err != nil {
			return fmt.Errorf("check search alias: %w", err)
		}
		if exists {
			return nil
		}
	}

	index, err := newPhysicalIndexName()
	if err != nil {
		return err
	}
	if err := reindexer.client.CreateIndex(ctx, index); err != nil {
		return fmt.Errorf("create search index: %w", err)
	}
	switched := false
	defer func() {
		if !switched {
			_ = reindexer.client.DeleteIndex(context.Background(), index)
		}
	}()

	h1, err := reindexer.store.HighWatermark(ctx)
	if err != nil {
		return err
	}
	if err := reindexer.copyRange(ctx, index, 0, h1); err != nil {
		return err
	}
	if err := reindexer.client.Refresh(ctx, index); err != nil {
		return fmt.Errorf("refresh search index before alias switch: %w", err)
	}
	mysqlCount, err := reindexer.store.CountUpTo(ctx, h1)
	if err != nil {
		return err
	}
	indexCount, err := reindexer.client.Count(ctx, index)
	if err != nil {
		return fmt.Errorf("count search index before alias switch: %w", err)
	}
	if mysqlCount != indexCount {
		return fmt.Errorf("search index count mismatch before alias switch: mysql=%d index=%d", mysqlCount, indexCount)
	}
	oldIndices, err := reindexer.client.SwitchAlias(ctx, index)
	if err != nil {
		return fmt.Errorf("switch search alias: %w", err)
	}
	switched = true

	h2, err := reindexer.store.HighWatermark(ctx)
	if err != nil {
		return err
	}
	if h2 > h1 {
		if err := reindexer.copyRange(ctx, index, h1, h2); err != nil {
			return err
		}
	}
	if err := reindexer.client.Refresh(ctx, index); err != nil {
		return fmt.Errorf("refresh search index after tail compensation: %w", err)
	}
	mysqlCount, err = reindexer.store.CountUpTo(ctx, h2)
	if err != nil {
		return err
	}
	indexCount, err = reindexer.client.Count(ctx, index)
	if err != nil {
		return fmt.Errorf("count search index after tail compensation: %w", err)
	}
	if mysqlCount != indexCount {
		return fmt.Errorf("search index count mismatch after tail compensation: mysql=%d index=%d", mysqlCount, indexCount)
	}
	for _, oldIndex := range oldIndices {
		if oldIndex == index {
			continue
		}
		if err := reindexer.client.DeleteIndex(ctx, oldIndex); err != nil {
			return fmt.Errorf("delete old search index: %w", err)
		}
	}
	return nil
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
		if err := reindexer.client.BulkIndex(ctx, index, documents); err != nil {
			return fmt.Errorf("bulk index posts: %w", err)
		}
		after = documents[len(documents)-1].PostID
	}
	return nil
}

func newPhysicalIndexName() (string, error) {
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate physical index name: %w", err)
	}
	return PhysicalIndexPrefix + time.Now().UTC().Format("20060102t150405000000000z") + "-" + hex.EncodeToString(random), nil
}
