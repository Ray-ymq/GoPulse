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
