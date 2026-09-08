//go:build integration

package migrations

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
)

func TestIntegrationNotificationShapeMigrationRoundTrip(t *testing.T) {
	cfg := integrationtest.Environment(t)
	db, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	name := fmt.Sprintf("nshape_%d", time.Now().UnixNano())
	posts, comments := name+"_posts", name+"_comments"
	exec := func(query string) {
		t.Helper()
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	exec("CREATE TABLE " + posts + " (id BIGINT PRIMARY KEY)")
	defer db.Exec("DROP TABLE " + posts)
	exec("CREATE TABLE " + comments + " (id BIGINT PRIMARY KEY)")
	defer db.Exec("DROP TABLE " + comments)
	exec("INSERT INTO " + posts + " VALUES (1)")
	exec("INSERT INTO " + comments + " VALUES (1)")
	exec("CREATE TABLE " + name + " (type VARCHAR(64) NOT NULL,post_id BIGINT NULL,comment_id BIGINT NULL, CONSTRAINT fk_" + name + "_post_deleted FOREIGN KEY(post_id) REFERENCES " + posts + "(id) ON DELETE SET NULL, CONSTRAINT fk_" + name + "_comment_deleted FOREIGN KEY(comment_id) REFERENCES " + comments + "(id) ON DELETE SET NULL)")
	defer db.Exec("DROP TABLE " + name)
	replace := strings.NewReplacer("notifications", name, "REFERENCES posts", "REFERENCES "+posts, "REFERENCES comments", "REFERENCES "+comments)
	for _, direction := range []string{"up", "down", "up"} {
		raw, err := files.ReadFile("000011_notification_shape." + direction + ".sql")
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range strings.Split(replace.Replace(string(raw)), ";") {
			if strings.TrimSpace(statement) != "" {
				exec(statement)
			}
		}
		if direction == "down" {
			continue
		}
		for _, values := range []string{"'comment.created',1,1", "'post.liked',1,NULL", "'user.followed',NULL,NULL", "'comment.created',NULL,NULL", "'post.liked',NULL,NULL"} {
			exec("INSERT INTO " + name + " VALUES (" + values + ")")
		}
		for _, values := range []string{"'comment.created',NULL,1", "'comment.created',1,NULL", "'post.liked',1,1", "'user.followed',1,NULL"} {
			if _, err := db.Exec("INSERT INTO " + name + " VALUES (" + values + ")"); err == nil {
				t.Fatalf("accepted invalid shape %s", values)
			}
		}
		if _, err := db.Exec("UPDATE " + name + " SET comment_id=NULL WHERE type='comment.created' AND post_id=1"); err == nil {
			t.Fatal("accepted partial tombstone update")
		}
		exec("UPDATE " + name + " SET post_id=NULL,comment_id=NULL WHERE post_id=1")
	}
	exec("DELETE FROM " + comments)
	exec("DELETE FROM " + posts)
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + name + " WHERE post_id IS NULL AND comment_id IS NULL").Scan(&count); err != nil || count != 10 {
		t.Fatalf("tombstones=%d err=%v", count, err)
	}
}
