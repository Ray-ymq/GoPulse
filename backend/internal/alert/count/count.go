// Package count implements the internal, bounded Elasticsearch count boundary.
package count

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type Performer interface {
	Perform(context.Context, *http.Request) (*http.Response, error)
}
type Catalog struct {
	Fields           map[string][]string `json:"fields"`
	Tuples           []map[string]string `json:"allowed_combinations"`
	Reducers         []string            `json:"reducers"`
	MinimumSelectors int                 `json:"minimum_selectors"`
}

var ErrUnknown = errors.New("count unavailable")

// Missing aliases remain unknown: absence alone cannot prove a fresh installation.
// No caller can supply a DSL, time expression, arbitrary field, or upstream path.
func Query(ctx context.Context, client Performer, alias string, fields map[string]string, from, to time.Time) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if !from.Before(to) || to.Sub(from) > 15*time.Minute {
		return 0, ErrUnknown
	}
	keys := []string{"@timestamp"}
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var caps struct {
		Fields map[string]map[string]struct {
			Type          string   `json:"type"`
			Searchable    bool     `json:"searchable"`
			NonSearchable []string `json:"non_searchable_indices"`
		} `json:"fields"`
	}
	if request(ctx, client, "/"+alias+"/_field_caps?fields="+url.QueryEscape(strings.Join(keys, ","))+"&ignore_unavailable=false&allow_no_indices=false", nil, &caps) != nil {
		return 0, ErrUnknown
	}
	for _, k := range keys {
		typ := "keyword"
		if k == "@timestamp" {
			typ = "date_nanos"
		}
		entries := caps.Fields[k]
		entry, ok := entries[typ]
		if !ok || len(entries) != 1 || entry.Type != typ || !entry.Searchable || len(entry.NonSearchable) > 0 {
			return 0, ErrUnknown
		}
	}
	filters := []any{map[string]any{"range": map[string]any{"@timestamp": map[string]string{"gte": from.UTC().Format(time.RFC3339Nano), "lte": to.UTC().Format(time.RFC3339Nano)}}}}
	for _, k := range keys {
		if k != "@timestamp" {
			filters = append(filters, map[string]any{"term": map[string]string{k: fields[k]}})
		}
	}
	body, _ := json.Marshal(map[string]any{"query": map[string]any{"bool": map[string]any{"filter": filters}}})
	var result struct {
		Count  *int64 `json:"count"`
		Shards *struct {
			Total      *int64 `json:"total"`
			Successful *int64 `json:"successful"`
			Failed     *int64 `json:"failed"`
		} `json:"_shards"`
	}
	if request(ctx, client, "/"+alias+"/_count?ignore_unavailable=false&allow_no_indices=false", body, &result) != nil {
		return 0, ErrUnknown
	}
	if result.Count == nil || *result.Count < 0 || *result.Count > 9007199254740991 || result.Shards == nil {
		return 0, ErrUnknown
	}
	s := result.Shards
	if s.Total == nil || s.Successful == nil || s.Failed == nil || *s.Total <= 0 || *s.Total != *s.Successful || *s.Failed != 0 {
		return 0, ErrUnknown
	}
	return *result.Count, nil
}
func request(ctx context.Context, client Performer, path string, body []byte, out any) error {
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
	}
	req, e := http.NewRequestWithContext(ctx, method, path, bytes.NewReader(body))
	if e != nil {
		return ErrUnknown
	}
	req.Header.Set("Content-Type", "application/json")
	res, e := client.Perform(ctx, req)
	if e != nil {
		return ErrUnknown
	}
	if res == nil || res.Body == nil {
		return ErrUnknown
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return ErrUnknown
	}
	data, e := io.ReadAll(io.LimitReader(res.Body, (64<<10)+1))
	if e != nil || len(data) > 64<<10 || json.Unmarshal(data, out) != nil {
		return ErrUnknown
	}
	return nil
}
