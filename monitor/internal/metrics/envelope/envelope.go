package envelope

import (
	"crypto/rand"
	"encoding/hex"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"strings"
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
	ProducerKind    string   `json:"producer_kind,omitempty"`
	ProducerID      string   `json:"producer_id,omitempty"`
	ProducerVersion string   `json:"producer_version,omitempty"`
	PluginID        string   `json:"plugin_id,omitempty"`
	PluginVersion   string   `json:"plugin_version,omitempty"`
	TargetID        string   `json:"target_id"`
	ScrapeStatus    string   `json:"scrape_status"`
	Samples         []Sample `json:"samples"`
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

// NewV2 retains the historical Redis sample/series identity while identifying
// the producer in transport metadata, never as extra Redis storage labels.
func NewV2(pluginID, version, status string, samples []Sample, timestamp time.Time) (Envelope, error) {
	e, err := New(pluginID, version, status, samples, timestamp)
	if err != nil {
		return e, err
	}
	e.SchemaVersion = 2
	e.Source = strings.TrimSuffix(pluginID, "-exporter")
	e.Payload.TargetID = pluginID + "-local"
	e.Payload.PluginID, e.Payload.PluginVersion = "", ""
	e.Payload.ProducerKind, e.Payload.ProducerID, e.Payload.ProducerVersion = "exporter_plugin", pluginID, version
	return e, nil
}

func NewComponent(id, version string, samples []Sample, at time.Time) (Envelope, error) {
	e, err := New(id, version, "success", samples, at)
	if err != nil {
		return Envelope{}, err
	}
	e.SchemaVersion = 2
	e.Source = id
	e.Payload.PluginID = ""
	e.Payload.PluginVersion = ""
	e.Payload.ProducerKind = "component"
	e.Payload.ProducerID = id
	e.Payload.ProducerVersion = version
	e.Payload.TargetID = componentmetrics.Target(id)
	return e, nil
}
