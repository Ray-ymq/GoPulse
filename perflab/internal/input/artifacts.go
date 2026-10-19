// Package input validates the private deterministic artifacts consumed by the
// k6 script. It intentionally contains no request scheduler or measurements.
package input

import (
	"encoding/json"
	"fmt"
	"os"
)

const CredentialsSchemaVersion = "gopulse.phase18.credentials.v1"

type User struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
}

type Credentials struct {
	SchemaVersion string `json:"schema_version"`
	Password      string `json:"password"`
	Users         []User `json:"users"`
}

type Corpus struct {
	SchemaVersion      string     `json:"schema_version"`
	Seed               uint64     `json:"seed"`
	Users              []User     `json:"users"`
	ReadPostIDs        []uint64   `json:"read_post_ids"`
	InteractionPostIDs []uint64   `json:"interaction_post_ids"`
	EditablePostIDs    [][]uint64 `json:"editable_post_ids"`
	DeletePostIDs      [][]uint64 `json:"delete_post_ids"`
}

func LoadCorpus(path string) (Corpus, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return Corpus{}, fmt.Errorf("read load corpus: %w", err)
	}
	var corpus Corpus
	if err := json.Unmarshal(encoded, &corpus); err != nil {
		return Corpus{}, fmt.Errorf("decode load corpus: %w", err)
	}
	if len(corpus.Users) == 0 || len(corpus.ReadPostIDs) == 0 || len(corpus.InteractionPostIDs) == 0 ||
		len(corpus.EditablePostIDs) != len(corpus.Users) || len(corpus.DeletePostIDs) != len(corpus.Users) {
		return Corpus{}, fmt.Errorf("load corpus is incomplete")
	}
	return corpus, nil
}

func LoadCredentials(path string) (Credentials, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return Credentials{}, fmt.Errorf("read credentials: %w", err)
	}
	var credentials Credentials
	if err := json.Unmarshal(encoded, &credentials); err != nil {
		return Credentials{}, fmt.Errorf("decode credentials: %w", err)
	}
	if credentials.SchemaVersion != CredentialsSchemaVersion || len(credentials.Users) == 0 || credentials.Password == "" {
		return Credentials{}, fmt.Errorf("credentials are incomplete")
	}
	return credentials, nil
}
