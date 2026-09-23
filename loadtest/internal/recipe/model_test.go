package recipe

import (
	"testing"
	"time"
)

func TestInspectIsDeterministicAndComplete(t *testing.T) {
	candidate := Candidate{Version: "2.0.1", Revision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ManifestSHA256: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	first := Inspect(Seed, candidate, time.Unix(1, 0))
	second := Inspect(Seed, candidate, time.Unix(2, 0))
	if first.Digest != second.Digest || first.Counts != second.Counts {
		t.Fatalf("descriptor is not deterministic: first=%+v second=%+v", first, second)
	}
	if got := first.Counts; got != ExpectedCounts() {
		t.Fatalf("counts=%+v", got)
	}
	if first.IDRanges["users"] != (IDRange{First: 1, Last: 5000}) || first.IDRanges["posts"] != (IDRange{First: 1, Last: 50000}) || first.IDRanges["comments"] != (IDRange{First: 1, Last: 100000}) {
		t.Fatalf("ID ranges=%+v", first.IDRanges)
	}
}

func TestDigestChangesWithSeedAndExcludesRuntimeValues(t *testing.T) {
	if Digest(Seed) == Digest(Seed+1) {
		t.Fatal("different seed produced the same digest")
	}
	if len(Digest(Seed)) != len("sha256:")+64 {
		t.Fatalf("unexpected digest %q", Digest(Seed))
	}
}

func TestDerivedFactsHaveNoSelfRelations(t *testing.T) {
	for id := uint64(1); id <= 100000; id++ {
		if commentAuthor(id) == postAuthor(commentPost(id)) {
			t.Fatalf("comment %d is self-authored", id)
		}
	}
	for id := uint64(1); id <= 200000; id++ {
		if likeUser(id) == postAuthor(likePost(id)) {
			t.Fatalf("like %d is self-authored", id)
		}
		if followFollower(id) == followTarget(id) {
			t.Fatalf("follow %d is self-referential", id)
		}
	}
}

func TestCorpusPartitionsSessionPostsWithoutOverlap(t *testing.T) {
	corpus := corpusFor(Seed)
	if len(corpus.Users) != SessionUsers || len(corpus.EditablePostIDs) != SessionUsers || len(corpus.DeletePostIDs) != SessionUsers {
		t.Fatalf("corpus user partitions are incomplete")
	}
	seen := map[uint64]bool{}
	for index := range corpus.Users {
		if len(corpus.EditablePostIDs[index])+len(corpus.DeletePostIDs[index]) != UsersPerSession {
			t.Fatalf("user %d post partition has wrong size", index)
		}
		for _, id := range append(corpus.EditablePostIDs[index], corpus.DeletePostIDs[index]...) {
			if seen[id] {
				t.Fatalf("post %d appears twice", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != SessionUsers*UsersPerSession {
		t.Fatalf("session post pool=%d", len(seen))
	}
}
