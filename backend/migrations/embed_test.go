package migrations

import (
	"io"
	"strings"
	"testing"
)

func TestEmbeddedPhaseOneMigrationContainsRequiredSchema(t *testing.T) {
	source, err := Source()
	if err != nil {
		t.Fatalf("Source() error = %v", err)
	}
	defer source.Close()

	version, err := source.First()
	if err != nil {
		t.Fatalf("First() error = %v", err)
	}
	if version != 1 {
		t.Fatalf("first version = %d, want 1", version)
	}

	reader, identifier, err := source.ReadUp(version)
	if err != nil {
		t.Fatalf("ReadUp() error = %v", err)
	}
	up, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatalf("read up migration: %v", err)
	}
	if identifier != "phase1_schema" {
		t.Fatalf("identifier = %q, want phase1_schema", identifier)
	}
	upSQL := string(up)
	for _, required := range []string{
		"CREATE TABLE users", "CREATE TABLE posts", "CREATE TABLE comments", "CREATE TABLE post_likes",
		"utf8mb4_0900_ai_ci", "DATETIME(6)", "ON UPDATE RESTRICT ON DELETE RESTRICT",
		"UNIQUE KEY uq_users_username", "idx_posts_created_at_id", "idx_comments_post_id_id", "idx_post_likes_user_id_post_id",
	} {
		if !strings.Contains(upSQL, required) {
			t.Fatalf("up migration missing %q", required)
		}
	}
	if !(strings.Index(upSQL, "CREATE TABLE users") < strings.Index(upSQL, "CREATE TABLE posts") &&
		strings.Index(upSQL, "CREATE TABLE posts") < strings.Index(upSQL, "CREATE TABLE comments") &&
		strings.Index(upSQL, "CREATE TABLE comments") < strings.Index(upSQL, "CREATE TABLE post_likes")) {
		t.Fatal("up migration table order is not users -> posts -> comments -> post_likes")
	}

	downReader, _, err := source.ReadDown(version)
	if err != nil {
		t.Fatalf("ReadDown() error = %v", err)
	}
	down, err := io.ReadAll(downReader)
	_ = downReader.Close()
	if err != nil {
		t.Fatalf("read down migration: %v", err)
	}
	downSQL := string(down)
	if !(strings.Index(downSQL, "DROP TABLE post_likes") < strings.Index(downSQL, "DROP TABLE comments") &&
		strings.Index(downSQL, "DROP TABLE comments") < strings.Index(downSQL, "DROP TABLE posts") &&
		strings.Index(downSQL, "DROP TABLE posts") < strings.Index(downSQL, "DROP TABLE users")) {
		t.Fatal("down migration table order is not post_likes -> comments -> posts -> users")
	}
}
