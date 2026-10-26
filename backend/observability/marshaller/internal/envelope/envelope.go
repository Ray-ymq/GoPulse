package envelope

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Ray-ymq/GoPulse/runtimekit"
	"github.com/Ray-ymq/GoPulse/runtimekit/metriccontract"
)

const (
	SchemaVersion = 2
	MetricsDataID = 1001
)

var (
	messageIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	semverPattern    = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
)

type PermanentError struct{ Code string }

func (e *PermanentError) Error() string { return "record permanently rejected: " + e.Code }

func Code(err error) string {
	var target *PermanentError
	if errors.As(err, &target) {
		return target.Code
	}
	return ""
}

func reject(code string) error { return &PermanentError{Code: code} }

type Sample struct {
	Name       string            `json:"name"`
	Kind       string            `json:"kind"`
	Labels     map[string]string `json:"labels"`
	Value      json.Number       `json:"value"`
	FloatValue float64           `json:"-"`
}

type MetricData struct {
	Dimension map[string]string      `json:"dimension"`
	Metrics   map[string]json.Number `json:"metrics"`
	Timestamp int64                  `json:"timestamp"`
}

type Payload struct {
	DataID    int          `json:"dataid"`
	Time      int64        `json:"time"`
	Timestamp int64        `json:"timestamp"`
	Data      []MetricData `json:"data"`

	// Legacy fields are populated only when decoding the pre-batch contract.
	// They are excluded from JSON so newly produced messages cannot regress.
	ProducerKind    string   `json:"-"`
	ProducerID      string   `json:"-"`
	ProducerVersion string   `json:"-"`
	PluginID        string   `json:"-"`
	PluginVersion   string   `json:"-"`
	TargetID        string   `json:"-"`
	ScrapeStatus    string   `json:"-"`
	Samples         []Sample `json:"-"`
}

type rawEnvelope struct {
	SchemaVersion int             `json:"schema_version"`
	MessageID     string          `json:"message_id"`
	Type          string          `json:"type"`
	Source        string          `json:"source"`
	Timestamp     string          `json:"timestamp"`
	Payload       json.RawMessage `json:"payload"`
}

type Envelope struct {
	SchemaVersion int
	MessageID     string
	Type          string
	Source        string
	Timestamp     time.Time
	Payload       Payload
	RawPayload    json.RawMessage
	TimestampText string
}

type Decoder struct {
	MaxBytes   int
	FutureSkew time.Duration
	Now        func() time.Time
}

func (d Decoder) Decode(key, value []byte) (Envelope, error) {
	if d.MaxBytes <= 0 {
		d.MaxBytes = 1 << 20
	}
	if len(value) > d.MaxBytes {
		return Envelope{}, reject("record_too_large")
	}
	if !utf8.Valid(value) {
		return Envelope{}, reject("invalid_utf8")
	}
	if err := checkUniqueObject(value); err != nil {
		return Envelope{}, reject("invalid_json")
	}
	var raw rawEnvelope
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return Envelope{}, reject("invalid_envelope")
	}
	if err := expectEOF(decoder); err != nil {
		return Envelope{}, reject("invalid_json")
	}
	if !messageIDPattern.MatchString(string(key)) || raw.MessageID != string(key) {
		return Envelope{}, reject("message_id_mismatch")
	}
	if raw.SchemaVersion != 2 && raw.SchemaVersion != 1 {
		return Envelope{}, reject("unsupported_envelope")
	}
	if !supported(raw.Type, raw.Source) {
		return Envelope{}, reject("unsupported_envelope")
	}
	payloadBytes := bytes.TrimSpace(raw.Payload)
	if len(payloadBytes) < 2 || payloadBytes[0] != '{' || payloadBytes[len(payloadBytes)-1] != '}' {
		return Envelope{}, reject("invalid_payload")
	}
	if raw.Timestamp == "" || !strings.HasSuffix(raw.Timestamp, "Z") {
		return Envelope{}, reject("invalid_timestamp")
	}
	timestamp, err := time.Parse(time.RFC3339Nano, raw.Timestamp)
	if err != nil || timestamp.Location() != time.UTC {
		return Envelope{}, reject("invalid_timestamp")
	}
	now := time.Now()
	if d.Now != nil {
		now = d.Now()
	}
	if timestamp.After(now.Add(d.FutureSkew)) {
		return Envelope{}, reject("timestamp_too_far_future")
	}

	message := Envelope{SchemaVersion: raw.SchemaVersion, MessageID: raw.MessageID, Type: raw.Type, Source: raw.Source, Timestamp: timestamp, TimestampText: raw.Timestamp, RawPayload: append(json.RawMessage(nil), raw.Payload...)}
	if raw.Type == "metrics" {
		fields := map[string]json.RawMessage{}
		if json.Unmarshal(raw.Payload, &fields) != nil {
			return Envelope{}, reject("invalid_payload")
		}
		// A schema-2 record with old producer fields is accepted only as a
		// migration input. Every newly produced schema-2 record is the batch
		// shape below.
		if raw.SchemaVersion == 1 || fields["samples"] != nil {
			payload, decodeErr := decodeLegacyMetrics(raw.Payload, raw.Source)
			if decodeErr != nil {
				return Envelope{}, decodeErr
			}
			if raw.SchemaVersion == 1 && raw.Source != "redis" {
				return Envelope{}, reject("unsupported_envelope")
			}
			message.Payload = payload
		} else {
			payload, decodeErr := decodeMetrics(raw.Payload, raw.Source, timestamp)
			if decodeErr != nil {
				return Envelope{}, decodeErr
			}
			message.Payload = payload
		}
	}
	return message, nil
}

func decodeMetrics(body []byte, source string, at time.Time) (Payload, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || len(fields) != 4 || fields["dataid"] == nil || fields["time"] == nil || fields["timestamp"] == nil || fields["data"] == nil {
		return Payload{}, reject("invalid_payload")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var payload Payload
	if err := decoder.Decode(&payload); err != nil || expectEOF(decoder) != nil {
		return Payload{}, reject("invalid_payload")
	}
	if err := validateMetricsPayload(source, &payload, at); err != nil {
		return Payload{}, err
	}
	payload.PluginID = source + "-exporter"
	payload.PluginVersion = ""
	payload.TargetID = source + "-exporter-local"
	payload.ScrapeStatus = "success"
	return payload, nil
}

func decodeLegacyMetrics(body []byte, source string) (Payload, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return Payload{}, reject("invalid_payload")
	}
	var legacy struct {
		ProducerKind    string   `json:"producer_kind,omitempty"`
		ProducerID      string   `json:"producer_id,omitempty"`
		ProducerVersion string   `json:"producer_version,omitempty"`
		PluginID        string   `json:"plugin_id,omitempty"`
		PluginVersion   string   `json:"plugin_version,omitempty"`
		TargetID        string   `json:"target_id"`
		ScrapeStatus    string   `json:"scrape_status"`
		Samples         []Sample `json:"samples"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(&legacy); err != nil || expectEOF(decoder) != nil {
		return Payload{}, reject("invalid_payload")
	}
	if len(fields) != 5 && len(fields) != 6 {
		return Payload{}, reject("invalid_producer")
	}
	if fields["samples"] == nil || fields["target_id"] == nil || fields["scrape_status"] == nil {
		return Payload{}, reject("invalid_producer")
	}
	if fields["plugin_id"] == nil && (fields["producer_kind"] == nil || fields["producer_id"] == nil || fields["producer_version"] == nil) {
		return Payload{}, reject("invalid_producer")
	}
	if legacy.ProducerKind == "exporter_plugin" && legacy.ProducerID != source+"-exporter" {
		return Payload{}, reject("invalid_producer")
	}
	payload := Payload{
		ProducerKind: legacy.ProducerKind, ProducerID: legacy.ProducerID, ProducerVersion: legacy.ProducerVersion,
		PluginID: legacy.PluginID, PluginVersion: legacy.PluginVersion, TargetID: legacy.TargetID,
		ScrapeStatus: legacy.ScrapeStatus, Samples: legacy.Samples,
	}
	if payload.PluginID == "" && payload.ProducerID != "" {
		payload.PluginID, payload.PluginVersion = payload.ProducerID, payload.ProducerVersion
	}
	if payload.ProducerKind == "component" {
		if payload.ProducerID != source || payload.TargetID != runtimekit.Target(source) {
			return Payload{}, reject("invalid_producer")
		}
		if err := validateTransport(source, &payload, false); err != nil {
			return Payload{}, err
		}
		return payload, nil
	}
	if err := validateLegacyMetricsPayload(payload); err != nil {
		return Payload{}, err
	}
	payload.Samples = append([]Sample(nil), payload.Samples...)
	return payload, nil
}

func supported(messageType, source string) bool {
	return (messageType == "metrics" && (source == "redis" || source == "mysql" || source == "rabbitmq" || source == "kafka" || source == "elasticsearch" || source == "victoriametrics")) || (messageType == "logs" && logSource(source)) || (messageType == "events" && source == "monitor")
}

func logSource(source string) bool {
	switch source {
	case "backend", "business-worker", "search-indexer", "search-reindex":
		return true
	default:
		return false
	}
}

func expectEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("trailing JSON")
	}
	return err
}

func checkUniqueObject(value []byte) error {
	dec := json.NewDecoder(bytes.NewReader(value))
	dec.UseNumber()
	if err := scanValue(dec); err != nil {
		return err
	}
	return expectEOF(dec)
}

func scanValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate key %q", key)
			}
			seen[key] = struct{}{}
			if err := scanValue(dec); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim('}') {
			return errors.New("invalid object end")
		}
	case '[':
		for dec.More() {
			if err := scanValue(dec); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim(']') {
			return errors.New("invalid array end")
		}
	default:
		return errors.New("unexpected delimiter")
	}
	return nil
}

func validateMetricsPayload(source string, p *Payload, at time.Time) error {
	if p.DataID != MetricsDataID || p.Time != at.Unix() || p.Timestamp != at.Unix() || len(p.Data) == 0 {
		return reject("invalid_metric_batch")
	}
	if len(p.Data) > metriccontract.MaxSamples {
		return reject("invalid_metric_batch")
	}
	checked := make([]metriccontract.Sample, 0, len(p.Data))
	for _, item := range p.Data {
		if item.Timestamp <= 0 || len(item.Metrics) == 0 || !metriccontract.ValidLabels(item.Dimension) {
			return reject("invalid_metric_batch")
		}
		for name, number := range item.Metrics {
			value, err := number.Float64()
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return reject("invalid_value")
			}
			checked = append(checked, metriccontract.Sample{Name: name, Kind: metricKind(source, name), Labels: item.Dimension, Value: value})
		}
	}
	if err := metriccontract.Validate(source, checked); err != nil {
		return reject("invalid_sample_set")
	}
	p.Samples = make([]Sample, len(checked))
	for i, sample := range checked {
		value, _ := json.Number(formatFloat(sample.Value)).Float64()
		p.Samples[i] = Sample{Name: sample.Name, Kind: sample.Kind, Labels: sample.Labels, Value: json.Number(formatFloat(value)), FloatValue: value}
	}
	return nil
}

func metricKind(source, name string) string {
	if strings.HasSuffix(name, "_total") || strings.HasSuffix(name, "_count") || strings.HasSuffix(name, "_bucket") {
		return "counter"
	}
	return "gauge"
}

func formatFloat(value float64) string {
	return fmt.Sprintf("%g", value)
}

func validateLegacyMetricsPayload(p Payload) error {
	source := strings.TrimSuffix(p.PluginID, "-exporter")
	if p.PluginID != source+"-exporter" || !semverPattern.MatchString(p.PluginVersion) || p.TargetID != p.PluginID+"-local" {
		return reject("invalid_payload_identity")
	}
	return validateTransport(source, &p, true)
}

// validatePayload keeps the package-level contract used by older tests and
// callers. New records are validated through validateMetricsPayload above.
func validatePayload(p *Payload) error { return validateLegacyMetricsPayload(*p) }

func validateTransport(source string, p *Payload, exporter bool) error {
	checked := make([]metriccontract.Sample, len(p.Samples))
	for i := range p.Samples {
		sample := &p.Samples[i]
		value, err := sample.Value.Float64()
		if err != nil {
			return reject("invalid_value")
		}
		sample.FloatValue = value
		checked[i] = metriccontract.Sample{Name: sample.Name, Kind: sample.Kind, Labels: sample.Labels, Value: value}
	}
	if err := metriccontract.Validate(source, checked); err != nil {
		return reject("invalid_sample_set")
	}
	if !exporter && p.ScrapeStatus != "success" {
		return reject("invalid_component_status")
	}
	if !exporter {
		return nil
	}
	expectedUp := 1.
	switch p.ScrapeStatus {
	case "success":
	case "target_unavailable":
		expectedUp = 0
		if len(p.Samples) != 1 {
			return reject("invalid_sample_set")
		}
	default:
		return reject("invalid_sample_set")
	}
	found := false
	for _, sample := range checked {
		if sample.Name != "gopulse_"+source+"_up" {
			continue
		}
		if found || sample.Kind != "gauge" || len(sample.Labels) != 0 || sample.Value != expectedUp {
			return reject("invalid_sample_set")
		}
		found = true
	}
	if !found {
		return reject("invalid_sample_set")
	}
	return nil
}

func CanonicalKey(s Sample) string { return metriccontract.Key(s.Name, s.Labels) }
