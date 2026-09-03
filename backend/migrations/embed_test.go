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

func TestEmbeddedBusinessOutboxMigrationContainsOnlyPhaseTwoOutboxSchema(t *testing.T) {
	source, err := Source()
	if err != nil {
		t.Fatalf("Source() error = %v", err)
	}
	defer source.Close()

	version, err := source.Next(1)
	if err != nil {
		t.Fatalf("Next(1) error = %v", err)
	}
	if version != 2 {
		t.Fatalf("next version = %d, want 2", version)
	}

	reader, identifier, err := source.ReadUp(version)
	if err != nil {
		t.Fatalf("ReadUp(2) error = %v", err)
	}
	up, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatalf("read outbox up migration: %v", err)
	}
	if identifier != "business_outbox" {
		t.Fatalf("identifier = %q, want business_outbox", identifier)
	}
	upSQL := string(up)
	for _, required := range []string{
		"CREATE TABLE business_outbox",
		"UNIQUE KEY uq_business_outbox_event_id",
		"idx_business_outbox_pending",
		"idx_business_outbox_lease_recovery",
		"idx_business_outbox_published_cleanup",
		"CONSTRAINT chk_business_outbox_event_type CHECK",
		"CONSTRAINT chk_business_outbox_schema_version CHECK",
		"CONSTRAINT chk_business_outbox_state CHECK",
		"payload JSON NOT NULL",
		"ENUM('pending', 'leased', 'published')",
	} {
		if !strings.Contains(upSQL, required) {
			t.Fatalf("outbox up migration missing %q", required)
		}
	}
	for _, forbidden := range []string{"CREATE TABLE notifications", "ALTER TABLE users", "ALTER TABLE posts", "ALTER TABLE comments", "ALTER TABLE post_likes"} {
		if strings.Contains(upSQL, forbidden) {
			t.Fatalf("outbox up migration unexpectedly contains %q", forbidden)
		}
	}

	downReader, downIdentifier, err := source.ReadDown(version)
	if err != nil {
		t.Fatalf("ReadDown(2) error = %v", err)
	}
	down, err := io.ReadAll(downReader)
	_ = downReader.Close()
	if err != nil {
		t.Fatalf("read outbox down migration: %v", err)
	}
	if downIdentifier != "business_outbox" {
		t.Fatalf("down identifier = %q, want business_outbox", downIdentifier)
	}
	if strings.TrimSpace(string(down)) != "DROP TABLE business_outbox;" {
		t.Fatalf("outbox down migration = %q", string(down))
	}
}

func TestEmbeddedNotificationsMigrationContainsIdempotencyAndRelationships(t *testing.T) {
	source, err := Source()
	if err != nil {
		t.Fatalf("Source() error = %v", err)
	}
	defer source.Close()
	version, err := source.Next(2)
	if err != nil || version != 3 {
		t.Fatalf("Next(2) = %d, %v; want 3", version, err)
	}
	reader, identifier, err := source.ReadUp(version)
	if err != nil {
		t.Fatalf("ReadUp(3) error = %v", err)
	}
	up, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatalf("read notifications migration: %v", err)
	}
	if identifier != "notifications" {
		t.Fatalf("identifier = %q", identifier)
	}
	upSQL := string(up)
	for _, required := range []string{
		"CREATE TABLE notifications", "UNIQUE KEY uq_notifications_source_event_id",
		"idx_notifications_recipient_created_id", "fk_notifications_recipient",
		"fk_notifications_actor", "fk_notifications_post", "fk_notifications_comment",
		"chk_notifications_type", "chk_notifications_comment_shape", "read_at DATETIME(6) NULL",
	} {
		if !strings.Contains(upSQL, required) {
			t.Fatalf("notifications migration missing %q", required)
		}
	}
	downReader, downIdentifier, err := source.ReadDown(version)
	if err != nil {
		t.Fatalf("ReadDown(3) error = %v", err)
	}
	down, err := io.ReadAll(downReader)
	_ = downReader.Close()
	if err != nil {
		t.Fatalf("read notifications down migration: %v", err)
	}
	if downIdentifier != "notifications" || strings.TrimSpace(string(down)) != "DROP TABLE notifications;" {
		t.Fatalf("notifications down migration = %q (%q)", string(down), downIdentifier)
	}
}

func TestEmbeddedPostCreatedOutboxMigrationPreservesPostFactsOnDown(t *testing.T) {
	source, err := Source()
	if err != nil {
		t.Fatalf("Source() error = %v", err)
	}
	defer source.Close()
	version, err := source.Next(3)
	if err != nil || version != 4 {
		t.Fatalf("Next(3) = %d, %v; want 4", version, err)
	}
	upReader, identifier, err := source.ReadUp(version)
	if err != nil {
		t.Fatalf("ReadUp(4) error = %v", err)
	}
	up, readErr := io.ReadAll(upReader)
	_ = upReader.Close()
	if readErr != nil || identifier != "post_created_outbox" {
		t.Fatalf("post-created up migration = %q, %v", identifier, readErr)
	}
	if !strings.Contains(string(up), "'post.created'") || !strings.Contains(string(up), "DROP CHECK chk_business_outbox_event_type") {
		t.Fatalf("post-created up migration = %q", string(up))
	}
	downReader, _, err := source.ReadDown(version)
	if err != nil {
		t.Fatalf("ReadDown(4) error = %v", err)
	}
	down, readErr := io.ReadAll(downReader)
	_ = downReader.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	downSQL := string(down)
	if !strings.Contains(downSQL, "DELETE FROM business_outbox WHERE event_type = 'post.created'") || strings.Contains(downSQL, "DELETE FROM posts") {
		t.Fatalf("post-created down migration = %q", downSQL)
	}
}

func TestEmbeddedUserRoleMigrationIsReversibleAndConstrained(t *testing.T) {
	source, err := Source()
	if err != nil {
		t.Fatalf("Source() error = %v", err)
	}
	defer source.Close()
	version, err := source.Next(4)
	if err != nil || version != 5 {
		t.Fatalf("Next(4) = %d, %v; want 5", version, err)
	}
	reader, identifier, err := source.ReadUp(version)
	if err != nil {
		t.Fatalf("ReadUp(5) error = %v", err)
	}
	up, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatalf("read user roles up migration: %v", err)
	}
	if identifier != "user_roles" {
		t.Fatalf("identifier = %q, want user_roles", identifier)
	}
	upSQL := string(up)
	for _, required := range []string{"ALTER TABLE users", "ADD COLUMN role", "ENUM('user', 'admin')", "NOT NULL", "DEFAULT 'user'"} {
		if !strings.Contains(upSQL, required) {
			t.Fatalf("user roles up migration missing %q", required)
		}
	}
	downReader, downIdentifier, err := source.ReadDown(version)
	if err != nil {
		t.Fatalf("ReadDown(5) error = %v", err)
	}
	down, err := io.ReadAll(downReader)
	_ = downReader.Close()
	if err != nil {
		t.Fatalf("read user roles down migration: %v", err)
	}
	if downIdentifier != "user_roles" || !strings.Contains(string(down), "DROP COLUMN role") {
		t.Fatalf("user roles down migration identifier=%q sql=%q", downIdentifier, string(down))
	}
}
