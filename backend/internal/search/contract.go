package search

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	AliasName           = "gopulse-post-search-v1"
	PhysicalIndexPrefix = "gopulse-post-search-v1-"
)

// Mapping is the single index contract shared by rebuild and future incremental indexing.
var Mapping = json.RawMessage(`{
  "settings":{"number_of_shards":1,"number_of_replicas":0},
  "mappings":{"dynamic":"strict","properties":{
    "post_id":{"type":"long"},
    "title":{"type":"text"},
    "content":{"type":"text"},
    "created_at":{"type":"date"},
    "updated_at":{"type":"date"}
  }}
}`)

type Document struct {
	PostID    uint64    `json:"post_id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (document Document) Validate() error {
	if document.PostID == 0 || strings.TrimSpace(document.Title) == "" || strings.TrimSpace(document.Content) == "" || document.CreatedAt.IsZero() || document.UpdatedAt.IsZero() {
		return fmt.Errorf("search document fields are invalid")
	}
	return nil
}
