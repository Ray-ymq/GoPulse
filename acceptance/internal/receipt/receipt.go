// Package receipt writes the versioned, private acceptance result contract.
package receipt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const Schema = "gopulse.acceptance.v1"

type Command struct {
	Args       []string `json:"args"`
	ExitCode   int      `json:"exit_code"`
	DurationMS int64    `json:"duration_ms"`
	Error      string   `json:"error,omitempty"`
}

type Document struct {
	Schema             string    `json:"schema"`
	Status             string    `json:"status"`
	Project            string    `json:"project"`
	Version            string    `json:"version"`
	Revision           string    `json:"revision"`
	StartedAt          string    `json:"started_at"`
	FinishedAt         string    `json:"finished_at"`
	CleanupPassed      bool      `json:"cleanup_passed"`
	IsolationPreserved bool      `json:"isolation_preserved"`
	Commands           []Command `json:"commands"`
	Error              string    `json:"error,omitempty"`
}

func New(project, version, revision string) Document {
	return Document{
		Schema:             Schema,
		Project:            project,
		Version:            version,
		Revision:           revision,
		StartedAt:          time.Now().UTC().Format(time.RFC3339Nano),
		CleanupPassed:      true,
		IsolationPreserved: true,
	}
}

func WriteAtomic(path string, document Document) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	document.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".receipt-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish acceptance receipt: %w", err)
	}
	return os.Chmod(path, 0o600)
}
