//go:build integration

package post

import (
	"context"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
)

func TestIntegrationListVariantsPlansAndPagination(t *testing.T) {
	cfg := integrationtest.Environment(t)
	db, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	release := integrationtest.AcquirePostFactsLock(t, db)
	defer release()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	users := []uint64{}
	for i := 0; i < 4; i++ {
		result, err := tx.Exec("INSERT INTO users(username,password_hash) VALUES(?, 'hash')", time.Now().Format("150405000000")+string(rune('a'+i)))
		if err != nil {
			t.Fatal(err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		users = append(users, uint64(id))
	}
	for _, author := range users[1:3] {
		if _, err := tx.Exec("INSERT INTO user_follows(follower_id,followed_id) VALUES(?,?)", users[0], author); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	expectedAuthor, expectedFeed, expectedBookmarks := map[uint64]bool{}, map[uint64]bool{}, map[uint64]bool{}
	for n, author := range users[1:] {
		for j := 0; j < 3; j++ {
			result, err := tx.Exec("INSERT INTO posts(author_id,title,content,created_at,updated_at) VALUES(?,'list','body',?,?)", author, at, at)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := result.LastInsertId()
			if err != nil {
				t.Fatal(err)
			}
			id := uint64(raw)
			if n == 0 {
				expectedAuthor[id] = true
			}
			if n < 2 {
				expectedFeed[id] = true
			}
			if j == 1 {
				expectedBookmarks[id] = true
				if _, err := tx.Exec("INSERT INTO post_bookmarks(user_id,post_id,created_at) VALUES(?,?,?)", users[0], id, at.Add(time.Duration(n)*time.Second)); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	repo := NewMySQLRepository(tx)
	for _, tc := range []struct {
		name     string
		options  ListOptions
		expected map[uint64]bool
	}{
		{"author", ListOptions{AuthorID: users[1], Limit: 2}, expectedAuthor},
		{"following", ListOptions{Following: true, Limit: 2}, expectedFeed},
		{"bookmarks", ListOptions{Bookmarks: true, Limit: 2}, expectedBookmarks},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query, args := listStatement(users[0], tc.options)
			var plan string
			if err := tx.QueryRow("EXPLAIN FORMAT=JSON "+query, args...).Scan(&plan); err != nil {
				t.Fatal(err)
			}
			t.Logf("%s plan=%s", tc.name, plan)
			seen := map[uint64]bool{}
			options := tc.options
			for {
				records, err := repo.List(ctx, users[0], options)
				if err != nil {
					t.Fatal(err)
				}
				more := len(records) > options.Limit
				if more {
					records = records[:options.Limit]
				}
				for _, record := range records {
					if seen[record.ID] || !tc.expected[record.ID] {
						t.Fatalf("unexpected or duplicate id=%d", record.ID)
					}
					seen[record.ID] = true
				}
				if !more {
					break
				}
				last := records[len(records)-1]
				timestamp := last.CreatedAt
				if options.Bookmarks {
					timestamp = last.BookmarkCreatedAt
				}
				options.Cursor = &Cursor{ID: last.ID, CreatedAt: timestamp}
			}
			if len(seen) != len(tc.expected) {
				t.Fatalf("seen=%v expected=%v", seen, tc.expected)
			}
		})
	}
}
