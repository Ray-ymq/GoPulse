package load

import (
	"net/http"
	"testing"
)

func testCorpus() Corpus {
	corpus := Corpus{SchemaVersion: "test", Seed: 1}
	for id := uint64(1); id <= 16; id++ {
		corpus.Users = append(corpus.Users, User{ID: id, Username: "user" + string(rune('a'+id-1))})
		corpus.EditablePostIDs = append(corpus.EditablePostIDs, []uint64{id * 10})
		corpus.DeletePostIDs = append(corpus.DeletePostIDs, []uint64{id*10 + 1})
	}
	for id := uint64(200); id < 260; id++ {
		corpus.ReadPostIDs = append(corpus.ReadPostIDs, id)
		corpus.InteractionPostIDs = append(corpus.InteractionPostIDs, id)
	}
	return corpus
}

func TestWorkloadHasExactCategoryMix(t *testing.T) {
	corpus := testCorpus()
	credentials := Credentials{SchemaVersion: CredentialsSchemaVersion, Password: "password", Users: corpus.Users}
	state := &vuState{id: 3, corpus: &corpus, credentials: &credentials}
	counts := map[Category]int{}
	for slot := uint64(0); slot < 100; slot++ {
		counts[state.request(slot).Category]++
	}
	expected := map[Category]int{
		CategoryRead: 45, CategorySearch: 10, CategoryNotification: 10,
		CategoryContentWrite: 15, CategoryInteraction: 15, CategorySession: 5,
	}
	for category, want := range expected {
		if counts[category] != want {
			t.Fatalf("category %s count=%d want=%d all=%v", category, counts[category], want, counts)
		}
	}
}

func TestWorkloadRoutesAreDeterministicAndCarryExpectedStatuses(t *testing.T) {
	corpus := testCorpus()
	credentials := Credentials{SchemaVersion: CredentialsSchemaVersion, Password: "password", Users: corpus.Users}
	first := &vuState{id: 4, corpus: &corpus, credentials: &credentials}
	second := &vuState{id: 4, corpus: &corpus, credentials: &credentials}
	for slot := uint64(0); slot < 100; slot++ {
		left, right := first.request(slot), second.request(slot)
		if left.Template != right.Template || left.Path != right.Path || left.Category != right.Category {
			t.Fatalf("slot %d differs: %#v %#v", slot, left, right)
		}
		if len(left.ExpectedStatuses) == 0 {
			t.Fatalf("slot %d has no expected status", slot)
		}
	}
}

func TestLoginRequestUsesOnlyAuthenticationFields(t *testing.T) {
	request := LoginRequest(User{ID: 1, Username: "alice"}, "private-password")
	if request.Method != http.MethodPost || request.Template != "POST /api/v1/auth/login" {
		t.Fatalf("request=%#v", request)
	}
	if string(request.Body) != `{"password":"private-password","username":"alice"}` {
		t.Fatalf("body=%s", request.Body)
	}
}
