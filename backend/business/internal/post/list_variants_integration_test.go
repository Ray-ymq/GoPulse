//go:build integration

package post

import (
	"context"
	"reflect"
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
			createdAt := at.Add(-time.Duration(j) * time.Second)
			result, err := tx.Exec("INSERT INTO posts(author_id,title,content,created_at,updated_at) VALUES(?,'list','body',?,?)", author, createdAt, createdAt)
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
		viewer   uint64
		options  ListOptions
		expected map[uint64]bool
	}{
		{"author", users[0], ListOptions{AuthorID: users[1], Limit: 2}, expectedAuthor},
		{"following", users[0], ListOptions{Following: true, Limit: 2}, expectedFeed},
		{"following_author", users[0], ListOptions{Following: true, AuthorID: users[1], Limit: 2}, expectedAuthor},
		{"following_unfollowed_author", users[0], ListOptions{Following: true, AuthorID: users[3], Limit: 2}, map[uint64]bool{}},
		{"following_empty", users[3], ListOptions{Following: true, Limit: 2}, map[uint64]bool{}},
		{"bookmarks", users[0], ListOptions{Bookmarks: true, Limit: 2}, expectedBookmarks},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query, args := listStatement(tc.viewer, tc.options)
			var plan string
			if err := tx.QueryRow("EXPLAIN FORMAT=JSON "+query, args...).Scan(&plan); err != nil {
				t.Fatal(err)
			}
			t.Logf("%s plan=%s", tc.name, plan)
			seen := map[uint64]bool{}
			options := tc.options
			var previous *Post
			for {
				records, err := repo.List(ctx, tc.viewer, options)
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
					if previous != nil {
						before, current := previous.CreatedAt, record.CreatedAt
						if options.Bookmarks {
							before, current = previous.BookmarkCreatedAt, record.BookmarkCreatedAt
						}
						if current.After(before) || (current.Equal(before) && record.ID >= previous.ID) {
							t.Fatalf("incorrect descending order: previous=%+v current=%+v", *previous, record)
						}
					}
					copy := record
					previous = &copy
					if options.Following {
						want, err := repo.FindByID(ctx, record.ID, tc.viewer)
						if err != nil {
							t.Fatal(err)
						}
						want.Author.Following = true
						want.BookmarkCreatedAt = want.CreatedAt
						if !reflect.DeepEqual(record, want) {
							t.Fatalf("list/detail mismatch: list=%+v detail=%+v", record, want)
						}
					}
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
