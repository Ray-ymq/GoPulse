package user

import (
	"encoding/json"
	"maps"
	"testing"
	"time"
)

func TestCachePublicProfileUsesCanonicalNameAndPublicFields(t *testing.T) {
	bio := ""
	record := publicProfile{ID: 7, Username: "Alice_42", DisplayName: "Alice", Bio: &bio, CreatedAt: time.Now()}
	key := normalizedProfileKey(record.Username)
	if key != "alice_42" || !validPublicProfile(key, record) || validPublicProfile("alice_43", record) {
		t.Fatalf("公开资料缓存键为 %q，记录为 %+v", key, record)
	}
	payload, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"id": true, "username": true, "display_name": true, "bio": true, "created_at": true}
	actual := make(map[string]bool, len(fields))
	for field := range fields {
		actual[field] = true
	}
	if !maps.Equal(actual, want) {
		t.Fatalf("公开资料缓存字段为 %v，预期为 %v", actual, want)
	}
}
