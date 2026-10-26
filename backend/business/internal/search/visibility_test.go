package search

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type visibilityCall struct {
	method string
	path   string
	body   string
}

type visibilityPerformer struct {
	epoch string
	calls []visibilityCall
}

func (performer *visibilityPerformer) Perform(_ context.Context, request *http.Request) (*http.Response, error) {
	body := ""
	if request.Body != nil {
		value, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		body = string(value)
	}
	performer.calls = append(performer.calls, visibilityCall{method: request.Method, path: request.URL.Path, body: body})
	index := PhysicalIndexPrefix + "20261012t100000z-test"
	switch len(performer.calls) {
	case 1:
		return visibilityResponse(http.StatusOK, `{"`+index+`":{}}`), nil
	case 2:
		return visibilityResponse(http.StatusOK, `{"`+index+`":{"mappings":{"_meta":{"gopulse_data_epoch":"`+performer.epoch+`"}}}}`), nil
	case 3:
		return visibilityResponse(http.StatusOK, `{"hits":{"hits":[{"_index":"`+index+`","_id":"17","_version":4,"_source":{"data_epoch":"`+performer.epoch+`","post_id":17,"content_revision":4}}]}}`), nil
	default:
		return visibilityResponse(http.StatusInternalServerError, `{}`), nil
	}
}

func TestCheckVisibilityResolvesEpochAndQueriesOneExactObject(t *testing.T) {
	epoch := "123e4567-e89b-42d3-a456-426614174000"
	performer := &visibilityPerformer{epoch: epoch}
	repository := NewElasticsearchRepository(performer)
	result, err := repository.CheckVisibility(context.Background(), epoch, 17, 4)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || result.Epoch != epoch || result.Version != 4 || result.Generation == "" {
		t.Fatalf("visibility result = %+v", result)
	}
	if len(performer.calls) != 3 {
		t.Fatalf("request count = %d, want 3", len(performer.calls))
	}
	if performer.calls[0].method != http.MethodGet || performer.calls[0].path != "/_alias/"+AliasName {
		t.Fatalf("alias request = %+v", performer.calls[0])
	}
	if performer.calls[1].method != http.MethodGet || !strings.HasSuffix(performer.calls[1].path, "/_mapping") {
		t.Fatalf("mapping request = %+v", performer.calls[1])
	}
	query := performer.calls[2]
	if query.method != http.MethodPost || !strings.HasSuffix(query.path, "/_search") || !strings.Contains(query.body, `"size":1`) || !strings.Contains(query.body, `"data_epoch":"`+epoch+`"`) || !strings.Contains(query.body, `"post_id":17`) || !strings.Contains(query.body, `"content_revision":4`) {
		t.Fatalf("visibility query = %+v", query)
	}
}

func TestCheckVisibilityReportsPhysicalEpochMismatch(t *testing.T) {
	requested := "123e4567-e89b-42d3-a456-426614174000"
	actual := "123e4567-e89b-42d3-a456-426614174001"
	performer := &visibilityPerformer{epoch: actual}
	repository := NewElasticsearchRepository(performer)
	result, err := repository.CheckVisibility(context.Background(), requested, 17, 4)
	if err != ErrVisibilityEpochMismatch || result.Epoch != actual || len(performer.calls) != 2 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, len(performer.calls))
	}
}

func visibilityResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
