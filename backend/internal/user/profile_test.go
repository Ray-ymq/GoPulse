package user

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

func TestNormalizeProfile(t *testing.T) {
	name, bio := "  显示😀  ", "  简介  "
	n, b, err := NormalizeProfile(ProfileInput{&name, &bio})
	if err != nil || n != "显示😀" || b != "简介" {
		t.Fatalf("normalize %q %q %v", n, b, err)
	}
	for _, value := range []string{" ", strings.Repeat("名", 65)} {
		if _, _, err := NormalizeProfile(ProfileInput{&value, &bio}); err == nil {
			t.Fatal("invalid name accepted")
		}
	}
	bio = strings.Repeat("介", 161)
	if _, _, err := NormalizeProfile(ProfileInput{&name, &bio}); err == nil {
		t.Fatal("long bio accepted")
	}
	if _, _, err := NormalizeProfile(ProfileInput{}); err == nil {
		t.Fatal("missing fields accepted")
	}
}

type searchFake struct{ ProfileRepository }

func (searchFake) SearchProfiles(context.Context, uint64, UserSearchOptions) ([]Profile, []int, error) {
	return []Profile{{ID: 1}, {ID: 2}}, []int{0, 1}, nil
}
func TestUserSearchSignedQueryBoundCursor(t *testing.T) {
	s := NewProfileService(searchFake{}, "test-secret")
	options, err := s.ParseSearch(url.Values{"q": {" alice "}, "limit": {"1"}})
	if err != nil {
		t.Fatal(err)
	}
	_, cursor, err := s.Search(context.Background(), 3, options)
	if err != nil || cursor == nil {
		t.Fatalf("cursor %v %v", cursor, err)
	}
	if _, err = s.ParseSearch(url.Values{"q": {"alice"}, "cursor": {*cursor}}); err != nil {
		t.Fatal(err)
	}
	for _, values := range []url.Values{{"q": {"bob"}, "cursor": {*cursor}}, {"q": {"alice"}, "cursor": {*cursor + "x"}}, {"q": {" "}}, {"q": {"alice"}, "limit": {"51"}}} {
		if _, err = s.ParseSearch(values); err == nil {
			t.Fatalf("accepted invalid query: %v", values)
		}
	}
}
