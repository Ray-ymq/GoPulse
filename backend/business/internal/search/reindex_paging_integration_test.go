//go:build integration

package search

import (
	"context"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
)

func TestIntegrationReindexPagesMergeCurrentEpochTombstones(t *testing.T) {
	cfg := integrationtest.Environment(t)
	db, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	connection, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	// 临时表仅对当前连接可见，不修改业务事实或其他测试的 epoch。
	for _, statement := range []string{
		`CREATE TEMPORARY TABLE posts (id BIGINT UNSIGNED PRIMARY KEY,title VARCHAR(80),content TEXT,created_at DATETIME(6),updated_at DATETIME(6),edited_at DATETIME(6),content_revision BIGINT UNSIGNED)`,
		`CREATE TEMPORARY TABLE business_environment (singleton INT PRIMARY KEY,data_epoch VARCHAR(36))`,
		`CREATE TEMPORARY TABLE post_search_tombstones (data_epoch VARCHAR(36),post_id BIGINT UNSIGNED,deleted_at DATETIME(6),content_revision BIGINT UNSIGNED,PRIMARY KEY(data_epoch,post_id))`,
		`INSERT INTO business_environment VALUES(1,'current')`,
	} {
		if _, err := connection.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	store := &ReindexStore{reader: connection}
	if maximum, err := store.HighWatermark(ctx); err != nil || maximum != 0 {
		t.Fatalf("空表最大 ID=%d error=%v", maximum, err)
	}
	for _, statement := range []string{
		`INSERT INTO posts VALUES(1,'one','body',NOW(6),NOW(6),NULL,1),(4,'four','edited',NOW(6),NOW(6),NOW(6),3),(7,'seven','body',NOW(6),NOW(6),NULL,1),(10000000,'late','body',NOW(6),NOW(6),NULL,1)`,
		`INSERT INTO post_search_tombstones VALUES('current',2,NOW(6),2),('current',6,NOW(6),4),('current',10000001,NOW(6),2),('old',99999999,NOW(6),2)`,
	} {
		if _, err := connection.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	maximum, err := store.HighWatermark(ctx)
	if err != nil || maximum != 10000001 {
		t.Fatalf("最大 ID=%d error=%v", maximum, err)
	}
	want := []uint64{1, 2, 4, 6, 7, 10000000, 10000001}
	index, after := 0, uint64(0)
	for {
		page, err := store.Scan(ctx, after, maximum, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		if len(page) > 2 {
			t.Fatal("分页超过请求上限")
		}
		for _, document := range page {
			if index >= len(want) || document.PostID != want[index] {
				t.Fatalf("分页序号=%d 文档=%+v", index, document)
			}
			deleted := document.PostID == 2 || document.PostID == 6 || document.PostID == 10000001
			if document.Deleted != deleted || document.CreatedAt.IsZero() ||
				(document.PostID == 4 && (document.EditedAt == nil || document.ContentRevision != 3)) {
				t.Fatalf("文档字段错误：%+v", document)
			}
			after = document.PostID
			index++
		}
	}
	if index != len(want) {
		t.Fatalf("读到 %d 个事实，期望 %d 个", index, len(want))
	}
	page, err := store.Scan(ctx, 2, 4, 10)
	if err != nil || len(page) != 1 || page[0].PostID != 4 {
		t.Fatalf("游标或最大 ID 边界错误：page=%+v error=%v", page, err)
	}
}
