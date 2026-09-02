package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const maximumResponseBytes = 8 << 20

var (
	ErrUnavailable        = errors.New("search unavailable")
	ErrPointInTimeExpired = errors.New("search point in time expired")
)

type Performer interface {
	Perform(context.Context, *http.Request) (*http.Response, error)
}

type ElasticsearchRepository struct {
	performer Performer
}

func NewElasticsearchRepository(performer Performer) *ElasticsearchRepository {
	return &ElasticsearchRepository{performer: performer}
}

type Hit struct {
	PostID    uint64
	Score     float64
	CreatedAt string
	ShardDoc  int64
}

type SearchResult struct {
	Generation  string
	PointInTime string
	Hits        []Hit
}

func (repository *ElasticsearchRepository) AliasExists(ctx context.Context) (bool, error) {
	response, err := repository.do(ctx, http.MethodHead, "/_alias/"+url.PathEscape(AliasName), nil)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false, ErrUnavailable
	}
	return true, nil
}

func (repository *ElasticsearchRepository) ResolveGeneration(ctx context.Context) (string, error) {
	response, err := repository.do(ctx, http.MethodGet, "/_alias/"+url.PathEscape(AliasName), nil)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", ErrUnavailable
	}
	var aliases map[string]json.RawMessage
	if err := decodeJSON(response.Body, &aliases); err != nil || len(aliases) != 1 {
		return "", ErrUnavailable
	}
	for index := range aliases {
		if !validPhysicalIndex(index) {
			return "", ErrUnavailable
		}
		return index, nil
	}
	return "", ErrUnavailable
}

func (repository *ElasticsearchRepository) OpenPointInTime(ctx context.Context, generation string) (string, error) {
	return repository.openPointInTime(ctx, generation, pointInTimeKeepAliveValue)
}

func (repository *ElasticsearchRepository) openPointInTime(ctx context.Context, generation, keepAlive string) (string, error) {
	if !validPhysicalIndex(generation) || keepAlive == "" {
		return "", ErrUnavailable
	}
	path := "/" + generation + "/_pit?keep_alive=" + url.QueryEscape(keepAlive) + "&allow_partial_search_results=false"
	response, err := repository.do(ctx, http.MethodPost, path, nil)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", ErrUnavailable
	}
	var payload struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(response.Body, &payload); err != nil || payload.ID == "" || len(payload.ID) > 4096 {
		return "", ErrUnavailable
	}
	return payload.ID, nil
}

func (repository *ElasticsearchRepository) Search(ctx context.Context, generation, pointInTime, query string, limit int, after *Hit) (SearchResult, error) {
	if !validPhysicalIndex(generation) || pointInTime == "" || len(pointInTime) > 4096 || limit < 1 || limit > 50 {
		return SearchResult{}, ErrUnavailable
	}
	body := map[string]any{
		"size":    limit + 1,
		"_source": false,
		"pit": map[string]string{
			"id": pointInTime, "keep_alive": pointInTimeKeepAliveValue,
		},
		"query": map[string]any{"multi_match": map[string]any{
			"query": query, "type": "best_fields", "fields": []string{"title^2", "content"},
		}},
		"sort": []any{
			map[string]any{"_score": map[string]string{"order": "desc"}},
			map[string]any{"created_at": map[string]string{"order": "desc", "format": "strict_date_optional_time_nanos"}},
			map[string]any{"post_id": map[string]string{"order": "desc"}},
			map[string]any{"_shard_doc": map[string]string{"order": "desc"}},
		},
	}
	if after != nil {
		body["search_after"] = []any{after.Score, after.CreatedAt, after.PostID, after.ShardDoc}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return SearchResult{}, ErrUnavailable
	}
	response, err := repository.do(ctx, http.MethodPost, "/_search", bytes.NewReader(encoded))
	if err != nil {
		return SearchResult{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return SearchResult{}, ErrPointInTimeExpired
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return SearchResult{}, ErrUnavailable
	}
	var payload struct {
		PointInTime string `json:"pit_id"`
		Hits        struct {
			Hits []struct {
				Index string            `json:"_index"`
				ID    string            `json:"_id"`
				Sort  []json.RawMessage `json:"sort"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := decodeJSON(response.Body, &payload); err != nil || payload.PointInTime == "" || len(payload.PointInTime) > 4096 {
		return SearchResult{}, ErrUnavailable
	}
	result := SearchResult{Generation: generation, PointInTime: payload.PointInTime, Hits: make([]Hit, 0, len(payload.Hits.Hits))}
	for _, raw := range payload.Hits.Hits {
		if raw.Index != generation || len(raw.Sort) != 4 {
			return SearchResult{}, ErrUnavailable
		}
		postID, err := strconv.ParseUint(raw.ID, 10, 64)
		if err != nil || postID == 0 {
			return SearchResult{}, ErrUnavailable
		}
		var hit Hit
		if err := json.Unmarshal(raw.Sort[0], &hit.Score); err != nil || hit.Score < 0 || hit.Score != hit.Score {
			return SearchResult{}, ErrUnavailable
		}
		if err := json.Unmarshal(raw.Sort[1], &hit.CreatedAt); err != nil || hit.CreatedAt == "" {
			return SearchResult{}, ErrUnavailable
		}
		var sortID json.Number
		if err := json.Unmarshal(raw.Sort[2], &sortID); err != nil || sortID.String() != raw.ID {
			return SearchResult{}, ErrUnavailable
		}
		var shardDoc json.Number
		if err := json.Unmarshal(raw.Sort[3], &shardDoc); err != nil {
			return SearchResult{}, ErrUnavailable
		}
		hit.ShardDoc, err = strconv.ParseInt(shardDoc.String(), 10, 64)
		if err != nil || hit.ShardDoc < 0 {
			return SearchResult{}, ErrUnavailable
		}
		hit.PostID = postID
		result.Hits = append(result.Hits, hit)
	}
	return result, nil
}

func (repository *ElasticsearchRepository) ClosePointInTime(ctx context.Context, pointInTime string) error {
	if pointInTime == "" || len(pointInTime) > 4096 {
		return ErrUnavailable
	}
	body, err := json.Marshal(map[string]string{"id": pointInTime})
	if err != nil {
		return ErrUnavailable
	}
	response, err := repository.do(ctx, http.MethodDelete, "/_pit", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ErrUnavailable
	}
	return nil
}

func (repository *ElasticsearchRepository) CreateIndex(ctx context.Context, index string) error {
	if !validPhysicalIndex(index) {
		return errors.New("invalid physical search index")
	}
	response, err := repository.do(ctx, http.MethodPut, "/"+index, bytes.NewReader(Mapping))
	return expectSuccess(response, err)
}

func (repository *ElasticsearchRepository) BulkIndex(ctx context.Context, index string, documents []Document) error {
	if !validPhysicalIndex(index) || len(documents) == 0 {
		return errors.New("invalid bulk request")
	}
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	for _, document := range documents {
		if err := document.Validate(); err != nil {
			return err
		}
		action := map[string]any{"index": map[string]string{"_index": index, "_id": strconv.FormatUint(document.PostID, 10)}}
		if err := encoder.Encode(action); err != nil {
			return err
		}
		if err := encoder.Encode(document); err != nil {
			return err
		}
	}
	response, err := repository.do(ctx, http.MethodPost, "/_bulk", &body)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ErrUnavailable
	}
	var payload struct {
		Errors bool `json:"errors"`
		Items  []map[string]struct {
			Status int             `json:"status"`
			Error  json.RawMessage `json:"error"`
		} `json:"items"`
	}
	if err := decodeJSON(response.Body, &payload); err != nil || payload.Errors || len(payload.Items) != len(documents) {
		return ErrUnavailable
	}
	for _, item := range payload.Items {
		operation, ok := item["index"]
		if !ok || operation.Status < 200 || operation.Status >= 300 || len(operation.Error) > 0 {
			return ErrUnavailable
		}
	}
	return nil
}

func (repository *ElasticsearchRepository) Refresh(ctx context.Context, index string) error {
	if !validPhysicalIndex(index) {
		return errors.New("invalid physical search index")
	}
	response, err := repository.do(ctx, http.MethodPost, "/"+index+"/_refresh", nil)
	return expectSuccess(response, err)
}

func (repository *ElasticsearchRepository) Count(ctx context.Context, index string) (uint64, error) {
	if !validPhysicalIndex(index) {
		return 0, errors.New("invalid physical search index")
	}
	response, err := repository.do(ctx, http.MethodGet, "/"+index+"/_count", nil)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, ErrUnavailable
	}
	var payload struct {
		Count uint64 `json:"count"`
	}
	if err := decodeJSON(response.Body, &payload); err != nil {
		return 0, ErrUnavailable
	}
	return payload.Count, nil
}

func (repository *ElasticsearchRepository) SwitchAlias(ctx context.Context, newIndex string) ([]string, error) {
	if !validPhysicalIndex(newIndex) {
		return nil, errors.New("invalid physical search index")
	}
	old, err := repository.aliasIndices(ctx)
	if err != nil {
		return nil, err
	}
	actions := make([]map[string]any, 0, len(old)+1)
	for _, index := range old {
		if index != newIndex {
			actions = append(actions, map[string]any{"remove": map[string]string{"index": index, "alias": AliasName}})
		}
	}
	actions = append(actions, map[string]any{"add": map[string]string{"index": newIndex, "alias": AliasName}})
	body, _ := json.Marshal(map[string]any{"actions": actions})
	response, err := repository.do(ctx, http.MethodPost, "/_aliases", bytes.NewReader(body))
	if err := expectSuccess(response, err); err != nil {
		return nil, err
	}
	return old, nil
}

func (repository *ElasticsearchRepository) DeleteIndex(ctx context.Context, index string) error {
	if !validPhysicalIndex(index) {
		return errors.New("refusing non-exact search index deletion")
	}
	response, err := repository.do(ctx, http.MethodDelete, "/"+index, nil)
	return expectSuccess(response, err)
}

func (repository *ElasticsearchRepository) aliasIndices(ctx context.Context) ([]string, error) {
	response, err := repository.do(ctx, http.MethodGet, "/_alias/"+url.PathEscape(AliasName), nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, ErrUnavailable
	}
	var payload map[string]json.RawMessage
	if err := decodeJSON(response.Body, &payload); err != nil {
		return nil, ErrUnavailable
	}
	indices := make([]string, 0, len(payload))
	for index := range payload {
		if !validPhysicalIndex(index) {
			return nil, ErrUnavailable
		}
		indices = append(indices, index)
	}
	return indices, nil
}

func (repository *ElasticsearchRepository) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	if repository == nil || repository.performer == nil {
		return nil, ErrUnavailable
	}
	request, err := http.NewRequestWithContext(ctx, method, path, body)
	if err != nil {
		return nil, ErrUnavailable
	}
	if body != nil {
		if path == "/_bulk" {
			request.Header.Set("Content-Type", "application/x-ndjson")
		} else {
			request.Header.Set("Content-Type", "application/json")
		}
	}
	response, err := repository.performer.Perform(ctx, request)
	if err != nil {
		return nil, ErrUnavailable
	}
	return response, nil
}

func expectSuccess(response *http.Response, err error) error {
	if err != nil {
		return err
	}
	if response == nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ErrUnavailable
	}
	return nil
}

func decodeJSON(reader io.Reader, target any) error {
	decoder := json.NewDecoder(io.LimitReader(reader, maximumResponseBytes+1))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("response contains trailing data")
	}
	return nil
}

func validPhysicalIndex(index string) bool {
	if !strings.HasPrefix(index, PhysicalIndexPrefix) || len(index) == len(PhysicalIndexPrefix) || len(index) > 255 {
		return false
	}
	for _, character := range index[len(PhysicalIndexPrefix):] {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return false
		}
	}
	return !strings.ContainsAny(index, "*?,/#\\")
}
