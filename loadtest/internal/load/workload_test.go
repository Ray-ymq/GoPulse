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

func TestCapacityWorkloadProfilePinsMixAndRouteStatuses(t *testing.T) {
	profile := DefaultWorkloadProfile()
	if err := ValidateWorkloadProfile(profile); err != nil {
		t.Fatal(err)
	}
	corpus := testCorpus()
	credentials := Credentials{SchemaVersion: CredentialsSchemaVersion, Password: "password", Users: corpus.Users}
	state := &vuState{id: 2, corpus: &corpus, credentials: &credentials, profile: &profile}
	for slot := uint64(0); slot < 1000; slot++ {
		if err := ValidateRequest(profile, state.request(slot)); err != nil {
			t.Fatalf("slot %d: %v", slot, err)
		}
	}
}

func TestCapacityWorkloadProfileRejectsRouteStatusDrift(t *testing.T) {
	profile := DefaultWorkloadProfile()
	profile.Routes[0].AllowedStatuses = []int{http.StatusCreated}
	if err := ValidateWorkloadProfile(profile); err == nil {
		t.Fatal("route status drift was accepted")
	}
}

func TestDiagnosticIdentityOmitsReadQueryAndSessionSecrets(t *testing.T) {
	login := LoginRequest(User{ID: 1, Username: "private-user"}, "private-password")
	if login.ObjectKey() != "" || login.ContentDigest() != "" {
		t.Fatal("session secrets became ledger metadata")
	}
	write := Request{Category: CategoryContentWrite, Method: "PATCH", Path: "/api/v1/posts/9", Body: []byte(`{"title":"title","content":"private body"}`)}
	if write.ObjectKey() != "/api/v1/posts/9" || !validDigest(write.ContentDigest()) {
		t.Fatal("write identity missing")
	}
}

func TestDiagnosticFollowContractMatchesObservedAPIAndPreservesPhase19(t *testing.T) {
	diagnostic, _, err := LoadProfile("../../phase20-capacity-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	historical, _, err := LoadProfile("../../capacity-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		profile WorkloadProfile
		status  int
	}{{diagnostic.Workload, 200}, {historical.Workload, 204}} {
		state := vuState{id: 0, profile: &item.profile, corpus: &Corpus{Users: []User{{ID: 1}, {ID: 2}}, InteractionPostIDs: []uint64{9}}}
		request := state.request(280)
		if request.Template != "PUT /api/v1/users/:userId/follow" || !request.ExpectedStatuses[item.status] || len(request.ExpectedStatuses) != 1 {
			t.Fatalf("follow contract changed: %+v", request)
		}
	}
}
