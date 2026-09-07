//go:build integration

package user_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/notification"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
)

func TestIntegrationFollowTransactionAndFollowing(t *testing.T) {
	cfg := integrationtest.Environment(t)
	db, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	release := integrationtest.AcquirePostFactsLock(t, db)
	defer release()
	ctx := context.Background()
	r := user.NewMySQLRepository(db)
	suffix := time.Now().Format("150405.000000")
	a, err := r.Create(ctx, "follow_a_"+suffix, "hash")
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Create(ctx, "follow_b_"+suffix, "hash")
	if err != nil {
		t.Fatal(err)
	}
	c, err := r.Create(ctx, "follow_c_"+suffix, "hash")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		db.Exec("DELETE FROM notifications WHERE actor_id IN (?, ?, ?)", a.ID, b.ID, c.ID)
		db.Exec("DELETE FROM business_outbox WHERE JSON_EXTRACT(payload,'$.actor_id') IN (?, ?, ?)", a.ID, b.ID, c.ID)
		db.Exec("DELETE FROM posts WHERE author_id IN (?, ?, ?)", a.ID, b.ID, c.ID)
		db.Exec("DELETE FROM users WHERE id IN (?, ?, ?)", a.ID, b.ID, c.ID)
	}()
	if err = r.SetFollow(ctx, b.ID, b.ID, true); err == nil {
		t.Fatal("self follow accepted")
	}
	if err = r.SetFollow(ctx, b.ID, ^uint64(0), true); err == nil {
		t.Fatal("missing target accepted")
	}
	if _, err = db.Exec("INSERT INTO user_follows(follower_id, followed_id) VALUES (?, ?)", a.ID, a.ID); err == nil {
		t.Fatal("database accepts self follow")
	}
	var group sync.WaitGroup
	errors := make(chan error, 4)
	for range 4 {
		group.Add(1)
		go func() { defer group.Done(); errors <- r.SetFollow(ctx, b.ID, a.ID, true) }()
	}
	group.Wait()
	close(errors)
	for err = range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM user_follows WHERE follower_id = ? AND followed_id = ?", b.ID, a.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("relations %d %v", count, err)
	}
	var payload []byte
	if err = db.QueryRow("SELECT COUNT(*), ANY_VALUE(payload) FROM business_outbox WHERE event_type='user.followed' AND JSON_EXTRACT(payload,'$.actor_id') = ?", b.ID).Scan(&count, &payload); err != nil || count != 1 {
		t.Fatalf("outbox %d %v", count, err)
	}
	var event bus.Envelope
	if err = json.Unmarshal(payload, &event); err != nil {
		t.Fatal(err)
	}
	notifications, _ := notification.NewRepository(db)
	for range 2 {
		if _, err = notifications.Insert(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	records, err := notifications.ListByRecipient(ctx, a.ID, notification.ListOptions{Limit: 20})
	if err != nil || len(records) != 1 || records[0].PostID != nil || records[0].Actor.ID != b.ID {
		t.Fatalf("notifications %#v %v", records, err)
	}
	if err = notifications.MarkRead(ctx, a.ID, records[0].ID); err != nil {
		t.Fatal(err)
	}
	if err = notifications.MarkRead(ctx, b.ID, records[0].ID); err == nil {
		t.Fatal("foreign notification accepted")
	}
	posts := post.NewMySQLRepository(db)
	for range 3 {
		if _, err = posts.Create(ctx, a.ID, "following", "content"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = posts.Create(ctx, c.ID, "not following", "content"); err != nil {
		t.Fatal(err)
	}
	service := post.NewService(posts)
	first, err := service.List(ctx, b.ID, post.ListOptions{Limit: 2, Following: true})
	if err != nil || len(first.Posts) != 2 || first.NextCursor == nil {
		t.Fatalf("first %#v %v", first, err)
	}
	boundary, err := post.DecodeCursor(*first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.List(ctx, b.ID, post.ListOptions{Limit: 2, Following: true, Cursor: &boundary})
	if err != nil || len(second.Posts) != 1 || second.NextCursor != nil || second.Posts[0].ID == first.Posts[1].ID {
		t.Fatalf("second %#v %v", second, err)
	}
	for _, p := range append(first.Posts, second.Posts...) {
		if p.Author.ID != a.ID || !p.Author.Following {
			t.Fatal("foreign author or missing state")
		}
	}
	// A second relation proves stable private-list continuation (including ties).
	if err = r.SetFollow(ctx, b.ID, c.ID, true); err != nil {
		t.Fatal(err)
	}
	db.Exec("UPDATE user_follows SET created_at = '2026-09-07 00:00:00' WHERE follower_id = ?", b.ID)
	list, next, err := r.Relations(ctx, b.ID, false, user.RelationOptions{Limit: 1})
	if err != nil || len(list) != 1 || next == nil {
		t.Fatalf("relations %#v %v", list, err)
	}
	relCursor, err := post.DecodeCursor(*next)
	if err != nil {
		t.Fatal(err)
	}
	rest, last, err := r.Relations(ctx, b.ID, false, user.RelationOptions{Limit: 1, Cursor: &user.RelationCursor{CreatedAt: relCursor.CreatedAt, ID: relCursor.ID}})
	if err != nil || len(rest) != 1 || rest[0].ID == list[0].ID || last != nil {
		t.Fatalf("relations continuation %#v %v", rest, err)
	}
	fans, _, err := r.Relations(ctx, a.ID, true, user.RelationOptions{Limit: 20})
	if err != nil || len(fans) != 1 || fans[0].ID != b.ID {
		t.Fatal("followers", err)
	}
	for range 2 {
		if err = r.SetFollow(ctx, b.ID, a.ID, false); err != nil {
			t.Fatal(err)
		}
	}
	following, err := r.Following(ctx, b.ID, a.ID)
	if err != nil || following {
		t.Fatal("unfollow", err)
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM business_outbox WHERE event_type='user.followed' AND JSON_EXTRACT(payload,'$.actor_id') = ?", b.ID).Scan(&count); err != nil || count != 2 {
		t.Fatal("unfollow emitted event", count, err)
	}
	// Cascading user deletion must remove the remaining relation.
	if _, err = db.Exec("DELETE FROM posts WHERE author_id = ?", c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("DELETE FROM users WHERE id = ?", c.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM user_follows WHERE follower_id = ?", b.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("orphan relation", count, err)
	}
	empty, err := service.List(ctx, b.ID, post.ListOptions{Limit: 20, Following: true})
	if err != nil || len(empty.Posts) != 0 {
		t.Fatal("unfollowed feed", err)
	}
}
