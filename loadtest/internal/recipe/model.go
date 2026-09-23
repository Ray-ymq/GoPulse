// Package recipe defines and materializes the deterministic Phase 18 data set.
package recipe

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash"
	"time"
)

const (
	SchemaVersion             = "gopulse.phase18.recipe.v1"
	Seed               uint64 = 18002005
	SessionUsers              = 1024
	UsersPerSession           = 10
	DeletePostsPerUser        = 8
)

// Counts is both the requested recipe and the exact post-generation assertion.
type Counts struct {
	Users         uint64 `json:"users"`
	Posts         uint64 `json:"posts"`
	Comments      uint64 `json:"comments"`
	PostLikes     uint64 `json:"post_likes"`
	UserFollows   uint64 `json:"user_follows"`
	PostBookmarks uint64 `json:"post_bookmarks"`
	Outbox        uint64 `json:"business_outbox"`
	Notifications uint64 `json:"notifications"`
}

func ExpectedCounts() Counts {
	return Counts{
		Users: 5000, Posts: 50000, Comments: 100000,
		PostLikes: 200000, UserFollows: 200000,
		PostBookmarks: 25000, Outbox: 550000, Notifications: 500000,
	}
}

// IDRange makes the first and last generated primary key explicit.
type IDRange struct {
	First uint64 `json:"first"`
	Last  uint64 `json:"last"`
}

func ExpectedIDRanges() map[string]IDRange {
	return map[string]IDRange{
		"users":    {First: 1, Last: 5000},
		"posts":    {First: 1, Last: 50000},
		"comments": {First: 1, Last: 100000},
	}
}

// Candidate binds a recipe receipt to the immutable product candidate.
type Candidate struct {
	Version        string `json:"version"`
	Revision       string `json:"revision"`
	ManifestSHA256 string `json:"manifest_sha256"`
}

// Receipt is safe to retain as evidence: it contains no password or hash.
type Receipt struct {
	SchemaVersion string             `json:"schema_version"`
	Seed          uint64             `json:"seed"`
	Candidate     Candidate          `json:"candidate"`
	Counts        Counts             `json:"counts"`
	IDRanges      map[string]IDRange `json:"id_ranges"`
	Digest        string             `json:"digest"`
	GeneratedAt   time.Time          `json:"generated_at"`
	DurationMS    int64              `json:"duration_ms"`
}

var referenceTime = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

func postAuthor(postID uint64) uint64 { return (postID-1)%5000 + 1 }
func postTitle(postID uint64) string  { return fmt.Sprintf("Phase18 Post %05d", postID) }
func postContent(postID uint64) string {
	return fmt.Sprintf("seed=%d deterministic post=%05d", Seed, postID)
}
func commentPost(commentID uint64) uint64 {
	return (commentID-1)%50000 + 1
}
func commentAuthor(commentID uint64) uint64 {
	return postAuthor(commentPost(commentID))%5000 + 1
}
func likePost(likeID uint64) uint64 { return (likeID-1)%50000 + 1 }
func likeUser(likeID uint64) uint64 {
	post := likePost(likeID)
	offset := (likeID - 1) / 50000
	return (postAuthor(post)-1+offset+1)%5000 + 1
}
func followFollower(followID uint64) uint64 { return (followID-1)%5000 + 1 }
func followTarget(followID uint64) uint64 {
	offset := (followID - 1) / 5000
	return (followFollower(followID)-1+offset+1)%5000 + 1
}
func bookmarkPost(bookmarkID uint64) uint64 { return bookmarkID }
func bookmarkUser(bookmarkID uint64) uint64 {
	return postAuthor(bookmarkID)%5000 + 1
}
func rowTime(sequence uint64) time.Time {
	return referenceTime.Add(time.Duration(sequence) * time.Microsecond)
}

func writeBytes(digest hash.Hash, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = digest.Write(size[:])
	_, _ = digest.Write(value)
}
func writeString(digest hash.Hash, value string) { writeBytes(digest, []byte(value)) }
func writeUint(digest hash.Hash, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = digest.Write(encoded[:])
}

// Digest is derived only from the seed and generated business facts. Runtime
// timestamps, passwords, hashes, and randomness are intentionally excluded.
func Digest(seed uint64) string {
	digest := sha256.New()
	writeString(digest, SchemaVersion)
	writeUint(digest, seed)
	writeString(digest, "users")
	for id := uint64(1); id <= 5000; id++ {
		writeUint(digest, id)
		writeString(digest, username(id))
	}
	writeString(digest, "posts")
	for id := uint64(1); id <= 50000; id++ {
		writeUint(digest, id)
		writeUint(digest, postAuthor(id))
		writeString(digest, postTitle(id))
		writeString(digest, postContent(id))
	}
	writeString(digest, "comments")
	for id := uint64(1); id <= 100000; id++ {
		writeUint(digest, id)
		writeUint(digest, commentPost(id))
		writeUint(digest, commentAuthor(id))
		writeString(digest, fmt.Sprintf("seed=%d comment=%06d", seed, id))
	}
	writeString(digest, "post_likes")
	for id := uint64(1); id <= 200000; id++ {
		writeUint(digest, likePost(id))
		writeUint(digest, likeUser(id))
	}
	writeString(digest, "user_follows")
	for id := uint64(1); id <= 200000; id++ {
		writeUint(digest, followFollower(id))
		writeUint(digest, followTarget(id))
	}
	writeString(digest, "post_bookmarks")
	for id := uint64(1); id <= 25000; id++ {
		writeUint(digest, bookmarkPost(id))
		writeUint(digest, bookmarkUser(id))
	}
	writeString(digest, "business_outbox")
	forEachOutboxEvent(seed, func(event outboxEvent) error {
		writeString(digest, event.Type)
		writeString(digest, event.EventID)
		writeUint(digest, event.ActorID)
		writeUint(digest, event.RecipientID)
		writeUint(digest, event.PostID)
		writeUint(digest, event.CommentID)
		return nil
	})
	return fmt.Sprintf("sha256:%x", digest.Sum(nil))
}

func username(id uint64) string { return fmt.Sprintf("phase18_u%05d", id) }

// Inspect computes the complete deterministic descriptor without touching MySQL.
func Inspect(seed uint64, candidate Candidate, now time.Time) Receipt {
	return Receipt{
		SchemaVersion: SchemaVersion,
		Seed:          seed,
		Candidate:     candidate,
		Counts:        ExpectedCounts(),
		IDRanges:      ExpectedIDRanges(),
		Digest:        Digest(seed),
		GeneratedAt:   now.UTC(),
	}
}
