package recipe

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const batchSize = 500

var ErrNonEmptyTarget = errors.New("recipe target contains business facts")

type CorpusUser struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
}

// Corpus is private runtime input for the load generator. It contains no
// password or session material.
type Corpus struct {
	SchemaVersion      string       `json:"schema_version"`
	Seed               uint64       `json:"seed"`
	Users              []CorpusUser `json:"users"`
	ReadPostIDs        []uint64     `json:"read_post_ids"`
	InteractionPostIDs []uint64     `json:"interaction_post_ids"`
	EditablePostIDs    [][]uint64   `json:"editable_post_ids"`
	DeletePostIDs      [][]uint64   `json:"delete_post_ids"`
}

type Credentials struct {
	SchemaVersion string       `json:"schema_version"`
	Password      string       `json:"password"`
	Users         []CorpusUser `json:"users"`
}

type GenerateOptions struct {
	DSN        string
	Seed       uint64
	Password   string
	Candidate  Candidate
	Receipt    string
	Corpus     string
	Credential string
	Now        func() time.Time
}

func Generate(ctx context.Context, database *sql.DB, options GenerateOptions) (Receipt, error) {
	if database == nil || strings.TrimSpace(options.DSN) != "" && database == nil {
		return Receipt{}, errors.New("recipe database is required")
	}
	if options.Seed == 0 || len([]byte(options.Password)) < 8 || len([]byte(options.Password)) > 72 {
		return Receipt{}, errors.New("recipe seed and password are invalid")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	started := now().UTC()
	if err := ensureEmpty(ctx, database); err != nil {
		return Receipt{}, err
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(options.Password), bcrypt.DefaultCost)
	if err != nil {
		return Receipt{}, errors.New("hash recipe password")
	}

	transaction, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Receipt{}, errors.New("begin recipe transaction")
	}
	defer transaction.Rollback()

	if err := insertUsers(ctx, transaction, options.Seed, string(passwordHash)); err != nil {
		return Receipt{}, err
	}
	if err := insertPosts(ctx, transaction); err != nil {
		return Receipt{}, err
	}
	if err := insertComments(ctx, transaction, options.Seed); err != nil {
		return Receipt{}, err
	}
	if err := insertLikes(ctx, transaction); err != nil {
		return Receipt{}, err
	}
	if err := insertFollows(ctx, transaction); err != nil {
		return Receipt{}, err
	}
	if err := insertBookmarks(ctx, transaction); err != nil {
		return Receipt{}, err
	}
	publishedAt := now().UTC()
	if err := insertOutbox(ctx, transaction, options.Seed, publishedAt); err != nil {
		return Receipt{}, err
	}
	if err := insertNotifications(ctx, transaction, options.Seed); err != nil {
		return Receipt{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Receipt{}, errors.New("commit recipe transaction")
	}
	if err := verifyMaterialized(ctx, database); err != nil {
		return Receipt{}, err
	}

	receipt := Inspect(options.Seed, options.Candidate, started)
	receipt.DurationMS = now().UTC().Sub(started).Milliseconds()
	if options.Receipt != "" {
		if err := writeJSONAtomic(options.Receipt, receipt, 0o600); err != nil {
			return Receipt{}, err
		}
	}
	if options.Corpus != "" {
		if err := writeJSONAtomic(options.Corpus, corpusFor(options.Seed), 0o600); err != nil {
			return Receipt{}, err
		}
	}
	if options.Credential != "" {
		credentials := Credentials{SchemaVersion: CredentialsSchemaVersion, Password: options.Password, Users: corpusFor(options.Seed).Users}
		if err := writeJSONAtomic(options.Credential, credentials, 0o600); err != nil {
			return Receipt{}, err
		}
	}
	return receipt, nil
}

func ensureEmpty(ctx context.Context, database *sql.DB) error {
	var users, posts, comments, likes, follows, bookmarks, notifications, outbox uint64
	err := database.QueryRowContext(ctx, `
SELECT
 (SELECT COUNT(*) FROM users), (SELECT COUNT(*) FROM posts), (SELECT COUNT(*) FROM comments),
 (SELECT COUNT(*) FROM post_likes), (SELECT COUNT(*) FROM user_follows),
 (SELECT COUNT(*) FROM post_bookmarks), (SELECT COUNT(*) FROM notifications),
 (SELECT COUNT(*) FROM business_outbox)`).Scan(
		&users, &posts, &comments, &likes, &follows, &bookmarks, &notifications, &outbox,
	)
	if err != nil {
		return fmt.Errorf("inspect recipe target: %w", err)
	}
	if users+posts+comments+likes+follows+bookmarks+notifications+outbox != 0 {
		return ErrNonEmptyTarget
	}
	return nil
}

func verifyMaterialized(ctx context.Context, database *sql.DB) error {
	expected := ExpectedCounts()
	var users, posts, comments, likes, follows, bookmarks, outbox, published, notifications, projected uint64
	var minUser, maxUser, minPost, maxPost, minComment, maxComment uint64
	err := database.QueryRowContext(ctx, `
SELECT
 (SELECT COUNT(*) FROM users), (SELECT COUNT(*) FROM posts), (SELECT COUNT(*) FROM comments),
 (SELECT COUNT(*) FROM post_likes), (SELECT COUNT(*) FROM user_follows),
 (SELECT COUNT(*) FROM post_bookmarks), (SELECT COUNT(*) FROM business_outbox),
 (SELECT COUNT(*) FROM business_outbox WHERE status = 'published'),
 (SELECT COUNT(*) FROM notifications),
 (SELECT COUNT(*) FROM notifications n JOIN business_outbox o ON o.event_id = n.source_event_id),
 (SELECT MIN(id) FROM users), (SELECT MAX(id) FROM users),
 (SELECT MIN(id) FROM posts), (SELECT MAX(id) FROM posts),
 (SELECT MIN(id) FROM comments), (SELECT MAX(id) FROM comments)`).Scan(
		&users, &posts, &comments, &likes, &follows, &bookmarks, &outbox,
		&published, &notifications, &projected,
		&minUser, &maxUser, &minPost, &maxPost, &minComment, &maxComment,
	)
	if err != nil {
		return fmt.Errorf("verify materialized recipe: %w", err)
	}
	if users != expected.Users || posts != expected.Posts || comments != expected.Comments ||
		likes != expected.PostLikes || follows != expected.UserFollows ||
		bookmarks != expected.PostBookmarks || outbox != expected.Outbox ||
		minUser != 1 || maxUser != 5000 || minPost != 1 || maxPost != 50000 ||
		minComment != 1 || maxComment != 100000 {
		return errors.New("materialized recipe counts or ID ranges differ from contract")
	}
	// The seeded Outbox is materialized already-published so the baseline starts
	// from a converged steady state; every notification must trace back to the
	// exact business event that produced it.
	if published != expected.Outbox || notifications != expected.Notifications || projected != expected.Notifications {
		return errors.New("materialized projections differ from contract")
	}
	return nil
}

func insertUsers(ctx context.Context, tx *sql.Tx, seed uint64, passwordHash string) error {
	return batches(ExpectedCounts().Users, func(start, end uint64) error {
		rows := make([][]any, 0, end-start+1)
		for id := start; id <= end; id++ {
			now := rowTime(id)
			rows = append(rows, []any{id, username(id), passwordHash, now, now})
		}
		return insertRows(ctx, tx, "users", []string{"id", "username", "password_hash", "created_at", "updated_at"}, rows)
	})
}

func insertPosts(ctx context.Context, tx *sql.Tx) error {
	return batches(ExpectedCounts().Posts, func(start, end uint64) error {
		rows := make([][]any, 0, end-start+1)
		for id := start; id <= end; id++ {
			now := rowTime(1000000 + id)
			rows = append(rows, []any{id, postAuthor(id), postTitle(id), postContent(id), now, now})
		}
		return insertRows(ctx, tx, "posts", []string{"id", "author_id", "title", "content", "created_at", "updated_at"}, rows)
	})
}

func insertComments(ctx context.Context, tx *sql.Tx, seed uint64) error {
	return batches(ExpectedCounts().Comments, func(start, end uint64) error {
		rows := make([][]any, 0, end-start+1)
		for id := start; id <= end; id++ {
			rows = append(rows, []any{id, commentPost(id), commentAuthor(id), fmt.Sprintf("seed=%d comment=%06d", seed, id), rowTime(2000000 + id)})
		}
		return insertRows(ctx, tx, "comments", []string{"id", "post_id", "author_id", "content", "created_at"}, rows)
	})
}

func insertLikes(ctx context.Context, tx *sql.Tx) error {
	return batches(ExpectedCounts().PostLikes, func(start, end uint64) error {
		rows := make([][]any, 0, end-start+1)
		for id := start; id <= end; id++ {
			rows = append(rows, []any{likePost(id), likeUser(id), rowTime(3000000 + id)})
		}
		return insertRows(ctx, tx, "post_likes", []string{"post_id", "user_id", "created_at"}, rows)
	})
}

func insertFollows(ctx context.Context, tx *sql.Tx) error {
	return batches(ExpectedCounts().UserFollows, func(start, end uint64) error {
		rows := make([][]any, 0, end-start+1)
		for id := start; id <= end; id++ {
			rows = append(rows, []any{followFollower(id), followTarget(id), rowTime(4000000 + id)})
		}
		return insertRows(ctx, tx, "user_follows", []string{"follower_id", "followed_id", "created_at"}, rows)
	})
}

func insertBookmarks(ctx context.Context, tx *sql.Tx) error {
	return batches(ExpectedCounts().PostBookmarks, func(start, end uint64) error {
		rows := make([][]any, 0, end-start+1)
		for id := start; id <= end; id++ {
			rows = append(rows, []any{bookmarkPost(id), bookmarkUser(id), rowTime(5000000 + id)})
		}
		return insertRows(ctx, tx, "post_bookmarks", []string{"post_id", "user_id", "created_at"}, rows)
	})
}

// insertOutbox materializes the deterministic Outbox already in the published
// state. The seeded facts represent an established system, so their events have
// been delivered; replaying 550,000 rows through the live dispatcher would only
// measure the unmodified single-replica baseline drain rate. published_at uses
// the generation time so the default 168h retention cannot immediately expire
// rows whose business timestamp is the fixed 2026-01-01 reference.
func insertOutbox(ctx context.Context, tx *sql.Tx, seed uint64, publishedAt time.Time) error {
	rows := make([][]any, 0, batchSize)
	flush := func() error {
		if len(rows) == 0 {
			return nil
		}
		err := insertRows(ctx, tx, "business_outbox", []string{
			"event_id", "event_type", "schema_version", "payload", "status", "available_at",
			"attempt_count", "lease_owner", "lease_expires_at", "published_at", "last_error",
			"created_at", "updated_at",
		}, rows)
		rows = rows[:0]
		return err
	}
	if err := forEachOutboxEvent(seed, func(event outboxEvent) error {
		if err := event.validate(); err != nil {
			return err
		}
		payload, err := event.payload()
		if err != nil {
			return fmt.Errorf("encode deterministic outbox payload: %w", err)
		}
		rows = append(rows, []any{
			event.EventID, event.Type, 1, payload, "published", event.OccurredAt,
			0, nil, nil, publishedAt, nil, event.OccurredAt, publishedAt,
		})
		if len(rows) == batchSize {
			return flush()
		}
		return nil
	}); err != nil {
		return err
	}
	return flush()
}

// insertNotifications materializes the notification projection of every
// seeded business event so the baseline starts converged. Each row is derived
// from the same deterministic fact that produced its Outbox event.
func insertNotifications(ctx context.Context, tx *sql.Tx, seed uint64) error {
	rows := make([][]any, 0, batchSize)
	flush := func() error {
		if len(rows) == 0 {
			return nil
		}
		err := insertRows(ctx, tx, "notifications", []string{
			"source_event_id", "type", "recipient_id", "actor_id",
			"post_id", "comment_id", "created_at",
		}, rows)
		rows = rows[:0]
		return err
	}
	if err := forEachNotification(seed, func(fact notificationFact) error {
		rows = append(rows, []any{
			fact.SourceEventID, fact.Type, fact.RecipientID, fact.ActorID,
			nullableID(fact.PostID), nullableID(fact.CommentID), fact.CreatedAt,
		})
		if len(rows) == batchSize {
			return flush()
		}
		return nil
	}); err != nil {
		return err
	}
	return flush()
}

func nullableID(id uint64) any {
	if id == 0 {
		return nil
	}
	return id
}

func batches(total uint64, visit func(start, end uint64) error) error {
	for start := uint64(1); start <= total; start += batchSize {
		end := start + batchSize - 1
		if end > total {
			end = total
		}
		if err := visit(start, end); err != nil {
			return err
		}
	}
	return nil
}

func insertRows(ctx context.Context, tx *sql.Tx, table string, columns []string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}
	group := "(" + strings.TrimSuffix(strings.Repeat("?,", len(columns)), ",") + ")"
	statement := "INSERT INTO " + table + " (" + strings.Join(columns, ",") + ") VALUES " +
		strings.TrimSuffix(strings.Repeat(group+",", len(rows)), ",")
	arguments := make([]any, 0, len(rows)*len(columns))
	for _, row := range rows {
		if len(row) != len(columns) {
			return errors.New("recipe insert row shape mismatch")
		}
		arguments = append(arguments, row...)
	}
	if _, err := tx.ExecContext(ctx, statement, arguments...); err != nil {
		return fmt.Errorf("insert deterministic %s rows: %w", table, err)
	}
	return nil
}

func corpusFor(seed uint64) Corpus {
	corpus := Corpus{SchemaVersion: SchemaVersion, Seed: seed}
	for id := uint64(1); id <= SessionUsers; id++ {
		corpus.Users = append(corpus.Users, CorpusUser{ID: id, Username: username(id)})
		editable := make([]uint64, 0, UsersPerSession-DeletePostsPerUser)
		deletable := make([]uint64, 0, DeletePostsPerUser)
		// postAuthor strides a user's posts across the whole ID range, so the
		// session user's own posts are not adjacent. Only the author may edit
		// or delete a post, so the corpus must offer exactly these IDs.
		for offset := uint64(0); offset < UsersPerSession; offset++ {
			postID := id + offset*userCount
			if offset < DeletePostsPerUser {
				deletable = append(deletable, postID)
			} else {
				editable = append(editable, postID)
			}
		}
		corpus.EditablePostIDs = append(corpus.EditablePostIDs, editable)
		corpus.DeletePostIDs = append(corpus.DeletePostIDs, deletable)
	}
	firstRead := uint64(SessionUsers*UsersPerSession + 1)
	for id := firstRead; id <= 50000; id++ {
		corpus.ReadPostIDs = append(corpus.ReadPostIDs, id)
		corpus.InteractionPostIDs = append(corpus.InteractionPostIDs, id)
	}
	return corpus
}

func writeJSONAtomic(path string, value any, mode os.FileMode) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return errors.New("encode recipe artifact")
	}
	encoded = append(encoded, '\n')
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, encoded, mode); err != nil {
		return fmt.Errorf("write recipe artifact: %w", err)
	}
	if err := os.Chmod(temporary, mode); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("protect recipe artifact: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("publish recipe artifact: %w", err)
	}
	return nil
}
