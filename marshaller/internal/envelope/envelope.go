package envelope

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"github.com/Ray-ymq/GoPulse/componentmetrics/metriccontract"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
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
type Payload struct {
	ProducerKind    string   `json:"producer_kind,omitempty"`
	ProducerID      string   `json:"producer_id,omitempty"`
	ProducerVersion string   `json:"producer_version,omitempty"`
	PluginID        string   `json:"plugin_id"`
	PluginVersion   string   `json:"plugin_version"`
	TargetID        string   `json:"target_id"`
	ScrapeStatus    string   `json:"scrape_status"`
	Samples         []Sample `json:"samples"`
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
	if !messageIDPattern.Match(key) || raw.MessageID != string(key) {
		return Envelope{}, reject("message_id_mismatch")
	}
	if (raw.SchemaVersion != 1 && !(raw.SchemaVersion == 2 && raw.Type == "metrics")) || !supported(raw.Type, raw.Source) {
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
	var metricsPayload Payload
	if raw.Type == "metrics" {
		payloadDecoder := json.NewDecoder(bytes.NewReader(raw.Payload))
		payloadDecoder.DisallowUnknownFields()
		payloadDecoder.UseNumber()
		if err := payloadDecoder.Decode(&metricsPayload); err != nil || expectEOF(payloadDecoder) != nil {
			return Envelope{}, reject("invalid_payload")
		}
		if raw.SchemaVersion == 2 {
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw.Payload, &fields) != nil || len(fields) != 6 || fields["producer_kind"] == nil || fields["producer_id"] == nil || fields["producer_version"] == nil || fields["target_id"] == nil || fields["scrape_status"] == nil || fields["samples"] == nil || !validProducer(raw.Source, metricsPayload) || !semverPattern.MatchString(metricsPayload.ProducerVersion) {
				return Envelope{}, reject("invalid_producer")
			}
			metricsPayload.PluginID, metricsPayload.PluginVersion = metricsPayload.ProducerID, metricsPayload.ProducerVersion
		} else {
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw.Payload, &fields) != nil || len(fields) != 5 || fields["plugin_id"] == nil || fields["plugin_version"] == nil || fields["target_id"] == nil || fields["scrape_status"] == nil || fields["samples"] == nil {
				return Envelope{}, reject("invalid_producer")
			}
		}
		if raw.SchemaVersion == 1 && raw.Source != "redis" {
			return Envelope{}, reject("unsupported_envelope")
		}
		if err := validateMetricsPayload(raw.Source, &metricsPayload); err != nil {
			return Envelope{}, err
		}
	}
	return Envelope{SchemaVersion: raw.SchemaVersion, MessageID: raw.MessageID, Type: raw.Type, Source: raw.Source, Timestamp: timestamp, TimestampText: raw.Timestamp, Payload: metricsPayload, RawPayload: append(json.RawMessage(nil), raw.Payload...)}, nil
}

func supported(messageType, source string) bool {
	return (messageType == "metrics" && (source == "redis" || source == "mysql" || source == "rabbitmq" || source == "kafka" || source == "elasticsearch" || source == "victoriametrics" || componentmetrics.IsComponent(source))) || (messageType == "logs" && logSource(source)) || (messageType == "events" && source == "monitor")
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
		if err != nil {
			return err
		}
		if end != json.Delim('}') {
			return errors.New("invalid object end")
		}
	case '[':
		for dec.More() {
			if err := scanValue(dec); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil {
			return err
		}
		if end != json.Delim(']') {
			return errors.New("invalid array end")
		}
	default:
		return errors.New("unexpected delimiter")
	}
	return nil
}

func validatePayload(p *Payload) error {
	source := strings.TrimSuffix(p.PluginID, "-exporter")
	if p.PluginID != source+"-exporter" || !semverPattern.MatchString(p.PluginVersion) || p.TargetID != p.PluginID+"-local" {
		return reject("invalid_payload_identity")
	}
	return validateTransport(source, p, true)
}

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
	if !exporter {
		if p.ScrapeStatus != "success" {
			return reject("invalid_component_status")
		}
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
	for _, s := range checked {
		if s.Name != "gopulse_"+source+"_up" {
			continue
		}
		if found || s.Kind != "gauge" || len(s.Labels) != 0 || s.Value != expectedUp {
			return reject("invalid_sample_set")
		}
		found = true
	}
	if !found {
		return reject("invalid_sample_set")
	}
	return nil
}

func canonicalKey(s Sample) string { return metriccontract.Key(s.Name, s.Labels) }
func CanonicalKey(s Sample) string { return canonicalKey(s) }

func validProducer(source string, p Payload) bool {
	if componentmetrics.IsComponent(source) {
		return p.ProducerKind == "component" && p.ProducerID == source && p.TargetID == componentmetrics.Target(source)
	}
	return p.ProducerKind == "exporter_plugin" && p.ProducerID == source+"-exporter"
}
func validateMetricsPayload(source string, p *Payload) error {
	if componentmetrics.IsComponent(source) {
		return validateTransport(source, p, false)
	}
	if p.PluginID != source+"-exporter" {
		return reject("invalid_payload_identity")
	}
	return validatePayload(p)
}
