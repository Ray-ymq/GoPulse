package envelope

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

const TargetID = "redis-exporter-local"

type Sample struct {
	Name   string            `json:"name"`
	Kind   string            `json:"kind"`
	Labels map[string]string `json:"labels"`
	Value  float64           `json:"value"`
}

type Payload struct {
	PluginID      string   `json:"plugin_id"`
	PluginVersion string   `json:"plugin_version"`
	TargetID      string   `json:"target_id"`
	ScrapeStatus  string   `json:"scrape_status"`
	Samples       []Sample `json:"samples"`
}

type Envelope struct {
	SchemaVersion int       `json:"schema_version"`
	MessageID     string    `json:"message_id"`
	Type          string    `json:"type"`
	Source        string    `json:"source"`
	Timestamp     time.Time `json:"timestamp"`
	Payload       Payload   `json:"payload"`
}

func New(pluginID, pluginVersion, status string, samples []Sample, timestamp time.Time) (Envelope, error) {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return Envelope{}, err
	}
	return Envelope{
		SchemaVersion: 1,
		MessageID:     hex.EncodeToString(id),
		Type:          "metrics",
		Source:        "redis",
		Timestamp:     timestamp.UTC(),
		Payload: Payload{
			PluginID: pluginID, PluginVersion: pluginVersion, TargetID: TargetID,
			ScrapeStatus: status, Samples: samples,
		},
	}, nil
}
