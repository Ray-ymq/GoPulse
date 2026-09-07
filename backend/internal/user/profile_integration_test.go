//go:build integration

package user

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
)

func TestIntegrationProfileDiscovery(t *testing.T) {
	cfg := integrationtest.Environment(t)
	db, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	r := NewMySQLRepository(db)
	s := NewProfileService(r, cfg.Auth.JWTSecret)
	token := "profile_" + time.Now().Format("150405000000")
	a, err := r.Create(ctx, token, "secret-hash")
	if err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", a.ID)
	if a.DisplayName != a.Username || a.Bio != "" {
		t.Fatalf("new defaults %#v", a)
	}
	b, err := r.Create(ctx, token+"_b", "hash")
	if err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", b.ID)
	name, bio := "  "+token+" 中文  ", " bio "
	updated, err := s.Update(ctx, a.ID, ProfileInput{&name, &bio})
	if err != nil || updated.Username != token || updated.Bio != "bio" || !updated.IsSelf {
		t.Fatalf("update %#v %v", updated, err)
	}
	public, err := s.Get(ctx, token, b.ID)
	if err != nil || public.IsSelf {
		t.Fatalf("public %#v %v", public, err)
	}
	encoded, _ := json.Marshal(public)
	if strings.Contains(string(encoded), "role") || strings.Contains(string(encoded), "hash") {
		t.Fatal("private fields leaked")
	}
	if _, err = s.Get(ctx, "missing_"+token, b.ID); err == nil {
		t.Fatal("missing user accepted")
	}
	options, err := s.ParseSearch(url.Values{"q": {token}, "limit": {"1"}})
	if err != nil {
		t.Fatal(err)
	}
	first, cursor, err := s.Search(ctx, b.ID, options)
	if err != nil || len(first) != 1 || first[0].ID != a.ID || cursor == nil {
		t.Fatalf("first %#v %v %v", first, cursor, err)
	}
	options, err = s.ParseSearch(url.Values{"q": {token}, "limit": {"1"}, "cursor": {*cursor}})
	if err != nil {
		t.Fatal(err)
	}
	second, next, err := s.Search(ctx, b.ID, options)
	if err != nil || len(second) != 1 || second[0].ID != b.ID || next != nil {
		t.Fatalf("second %#v %v %v", second, next, err)
	}
	for _, query := range []string{strings.TrimSpace(name), token[2:]} {
		options, err = s.ParseSearch(url.Values{"q": {query}})
		if err != nil {
			t.Fatal(err)
		}
		records, _, err := s.Search(ctx, b.ID, options)
		if err != nil || len(records) == 0 {
			t.Fatalf("display/partial search %v %v", records, err)
		}
	}
	options, _ = s.ParseSearch(url.Values{"q": {"%" + token}})
	records, _, err := s.Search(ctx, b.ID, options)
	if err != nil || len(records) != 0 {
		t.Fatalf("LIKE escape %v %v", records, err)
	}
}
