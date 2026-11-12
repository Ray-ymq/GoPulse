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
    "data_epoch":{"type":"keyword"},
    "deleted":{"type":"boolean"},
    "post_id":{"type":"long"},
    "title":{"type":"text"},
    "content":{"type":"text"},
    "created_at":{"type":"date"},
    "updated_at":{"type":"date"},
    "edited_at":{"type":"date"},
    "content_revision":{"type":"long"}
  }}
}`)

type Document struct {
	Deleted         bool       `json:"deleted,omitempty"`
	DataEpoch       string     `json:"data_epoch,omitempty"`
	EditedAt        *time.Time `json:"edited_at"`
	ContentRevision uint64     `json:"content_revision"`
	PostID          uint64     `json:"post_id"`
	Title           string     `json:"title"`
	Content         string     `json:"content"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (document Document) Validate() error {
	if document.DataEpoch != "" && !validEpoch(document.DataEpoch) {
		return fmt.Errorf("search document epoch is invalid")
	}
	if document.Deleted {
		if document.PostID == 0 || document.ContentRevision == 0 {
			return fmt.Errorf("invalid search tombstone")
		}
		return nil
	}
	if document.PostID == 0 || strings.TrimSpace(document.Title) == "" || strings.TrimSpace(document.Content) == "" || document.CreatedAt.IsZero() || document.UpdatedAt.IsZero() {
		return fmt.Errorf("search document fields are invalid")
	}
	return nil
}

func validEpoch(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return value[14] >= '1' && value[14] <= '5' && (value[19] == '8' || value[19] == '9' || value[19] == 'a' || value[19] == 'b')
}
