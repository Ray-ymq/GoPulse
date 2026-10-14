package main

import (
	"net/http"
	"net/url"
	"testing"
)

func TestEffectRequestSelectorOnlyTargetsSearchProjectionWrites(t *testing.T) {
	target, _ := url.Parse("http://elasticsearch:9200")
	request, _ := http.NewRequest(http.MethodPut, "http://proxy/gopulse-post-search-v1/_doc/42", nil)
	if !isEffectRequest(target, request, http.StatusCreated) {
		t.Fatal("projection write was not selected")
	}
	for _, test := range []struct {
		method string
		path   string
		status int
	}{
		{method: http.MethodGet, path: "/gopulse-post-search-v1/_doc/42", status: http.StatusOK},
		{method: http.MethodPut, path: "/other-index/_doc/42", status: http.StatusCreated},
		{method: http.MethodPut, path: "/gopulse-post-search-v1/_doc/42", status: http.StatusBadGateway},
	} {
		candidate, _ := http.NewRequest(test.method, "http://proxy"+test.path, nil)
		if isEffectRequest(target, candidate, test.status) {
			t.Fatalf("unexpected effect selection for %#v", test)
		}
	}
}

func TestStoreDropsOnlyConfiguredPostProjectionWrites(t *testing.T) {
	state := &store{value: status{Mode: "effect-before-completion"}}
	if state.shouldDrop(newEffectRequest("42"), http.StatusCreated) {
		t.Fatal("unconfigured projection write was dropped")
	}
	if err := state.setPostTarget(42); err != nil {
		t.Fatal(err)
	}
	if !state.shouldDrop(newEffectRequest("42"), http.StatusCreated) {
		t.Fatal("configured projection write was not dropped")
	}
	if state.shouldDrop(newEffectRequest("43"), http.StatusCreated) {
		t.Fatal("different projection write was dropped")
	}
	if !state.shouldDrop(newEffectRequest("42"), http.StatusCreated) {
		t.Fatal("configured projection retry was not dropped")
	}
	value := state.snapshot()
	if value.TargetPostID != 42 || !value.TargetHit || value.EffectObserved != 2 || value.ResponseDropped != 2 || value.ConnectionClosed != 2 {
		t.Fatalf("unexpected target status: %#v", value)
	}
}

func newEffectRequest(postID string) *http.Request {
	request, _ := http.NewRequest(http.MethodPut, "http://proxy/gopulse-post-search-v1/_doc/"+postID, nil)
	return request
}
