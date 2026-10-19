package envelope

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"github.com/Ray-ymq/GoPulse/componentmetrics/metriccontract"
)

const (
	SchemaVersion = 2
	MetricsDataID = 1001
	TargetID      = "redis-exporter-local"
)

// Sample is the internal representation used while parsing Prometheus text.
// The wire format groups these samples into MetricData and intentionally does
// not expose producer or target identity in the payload.
type Sample struct {
	Name   string            `json:"name"`
	Kind   string            `json:"kind"`
	Labels map[string]string `json:"labels"`
	Value  float64           `json:"value"`
}

type MetricData struct {
	Dimension map[string]string  `json:"dimension"`
	Metrics   map[string]float64 `json:"metrics"`
	Timestamp int64              `json:"timestamp"`
}

type Payload struct {
	DataID    int          `json:"dataid"`
	Time      int64        `json:"time"`
	Timestamp int64        `json:"timestamp"`
	Data      []MetricData `json:"data"`

	// These fields are retained for in-process callers during the wire-format
	// migration. They are never serialized into the payload.
	ProducerKind    string   `json:"-"`
	ProducerID      string   `json:"-"`
	ProducerVersion string   `json:"-"`
	PluginID        string   `json:"-"`
	PluginVersion   string   `json:"-"`
	TargetID        string   `json:"-"`
	ScrapeStatus    string   `json:"-"`
	Samples         []Sample `json:"-"`
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
	source := strings.TrimSuffix(pluginID, "-exporter")
	if source == "" {
		source = "redis"
	}
	message, err := NewSource(source, samples, timestamp)
	if err != nil {
		return Envelope{}, err
	}
	message.Payload.PluginID = pluginID
	message.Payload.PluginVersion = pluginVersion
	message.Payload.ScrapeStatus = status
	message.Payload.TargetID = source + "-exporter-local"
	return message, nil
}

// NewV2 retains the old constructor for callers that have not yet migrated to
// NewSource. The resulting JSON always uses the new batch payload.
func NewV2(pluginID, version, status string, samples []Sample, timestamp time.Time) (Envelope, error) {
	return New(pluginID, version, status, samples, timestamp)
}

// NewV2WithIdentity keeps the old API shape while taking source identity from
// a plugin manifest. Producer metadata is kept only in memory and is not put
// on the wire.
func NewV2WithIdentity(pluginID, version, source, targetID, status string, samples []Sample, timestamp time.Time) (Envelope, error) {
	if source == "" {
		source = strings.TrimSuffix(pluginID, "-exporter")
	}
	message, err := NewSource(source, samples, timestamp)
	if err != nil {
		return Envelope{}, err
	}
	message.Payload.ProducerKind = "exporter_plugin"
	message.Payload.ProducerID = pluginID
	message.Payload.ProducerVersion = version
	message.Payload.PluginID = pluginID
	message.Payload.PluginVersion = version
	message.Payload.TargetID = targetID
	message.Payload.ScrapeStatus = status
	return message, nil
}

func NewSource(source string, samples []Sample, at time.Time) (Envelope, error) {
	checked := make([]metriccontract.Sample, len(samples))
	for i, sample := range samples {
		checked[i] = metriccontract.Sample{Name: sample.Name, Kind: sample.Kind, Labels: sample.Labels, Value: sample.Value}
	}
	if err := metriccontract.Validate(source, checked); err != nil {
		return Envelope{}, err
	}

	at = at.UTC()
	payload := Payload{DataID: MetricsDataID, Time: at.Unix(), Timestamp: at.Unix()}
	groups := make(map[string]int, len(samples))
	for _, sample := range samples {
		key := metriccontract.Key("", sample.Labels)
		index, exists := groups[key]
		if !exists {
			labels := make(map[string]string, len(sample.Labels))
			for name, value := range sample.Labels {
				labels[name] = value
			}
			payload.Data = append(payload.Data, MetricData{
				Dimension: labels,
				Metrics:   map[string]float64{},
				Timestamp: at.UnixMilli(),
			})
			index = len(payload.Data) - 1
			groups[key] = index
		}
		payload.Data[index].Metrics[sample.Name] = sample.Value
	}

	id, err := messageID()
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{
		SchemaVersion: SchemaVersion,
		MessageID:     id,
		Type:          "metrics",
		Source:        source,
		Timestamp:     at,
		Payload:       payloadWithLegacy(payload, samples),
	}, nil
}

func NewComponent(id, version string, samples []Sample, at time.Time) (Envelope, error) {
	message, err := NewSource(id, samples, at)
	if err != nil {
		return Envelope{}, err
	}
	message.Payload.ProducerKind = "component"
	message.Payload.ProducerID = id
	message.Payload.ProducerVersion = version
	message.Payload.TargetID = componentmetrics.Target(id)
	return message, nil
}

func payloadWithLegacy(payload Payload, samples []Sample) Payload {
	payload.Samples = append([]Sample(nil), samples...)
	return payload
}

func messageID() (string, error) {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return "", errors.New("message id generation failed")
	}
	return hex.EncodeToString(id), nil
}
