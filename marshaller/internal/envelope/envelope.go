package envelope

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
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
	PluginID      string   `json:"plugin_id"`
	PluginVersion string   `json:"plugin_version"`
	TargetID      string   `json:"target_id"`
	ScrapeStatus  string   `json:"scrape_status"`
	Samples       []Sample `json:"samples"`
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
	if raw.SchemaVersion != 1 || !supported(raw.Type, raw.Source) {
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
		if err := validatePayload(&metricsPayload); err != nil {
			return Envelope{}, err
		}
	}
	return Envelope{SchemaVersion: raw.SchemaVersion, MessageID: raw.MessageID, Type: raw.Type, Source: raw.Source, Timestamp: timestamp, TimestampText: raw.Timestamp, Payload: metricsPayload, RawPayload: append(json.RawMessage(nil), raw.Payload...)}, nil
}

func supported(messageType, source string) bool {
	return (messageType == "metrics" && source == "redis") || (messageType == "logs" && source == "backend")
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

type familyRule struct {
	kind    string
	labels  []string
	count   int
	counter bool
}

var successRules = map[string]familyRule{
	"gopulse_redis_up":                       {kind: "gauge", count: 1},
	"gopulse_redis_uptime_seconds":           {kind: "gauge", count: 1},
	"gopulse_redis_connected_clients":        {kind: "gauge", count: 1},
	"gopulse_redis_used_memory_bytes":        {kind: "gauge", count: 1},
	"gopulse_redis_commands_processed_total": {kind: "counter", count: 1, counter: true},
	"gopulse_redis_keyspace_hits_total":      {kind: "counter", count: 1, counter: true},
	"gopulse_redis_keyspace_misses_total":    {kind: "counter", count: 1, counter: true},
	"gopulse_redis_cpu_seconds_total":        {kind: "counter", labels: []string{"mode"}, count: 2, counter: true},
	"gopulse_redis_db_keys":                  {kind: "gauge", labels: []string{"db"}, count: 1},
	"gopulse_redis_db_expiring_keys":         {kind: "gauge", labels: []string{"db"}, count: 1},
}

func validatePayload(p *Payload) error {
	if p.PluginID != "redis-exporter" || !semverPattern.MatchString(p.PluginVersion) || p.TargetID != "redis-exporter-local" {
		return reject("invalid_payload_identity")
	}
	if p.Samples == nil || len(p.Samples) == 0 || len(p.Samples) > 1024 {
		return reject("invalid_sample_set")
	}
	if p.ScrapeStatus == "target_unavailable" {
		if len(p.Samples) != 1 {
			return reject("invalid_sample_set")
		}
		s := &p.Samples[0]
		if err := validateSample(s, familyRule{kind: "gauge", count: 1}); err != nil {
			return err
		}
		if s.Name != "gopulse_redis_up" || len(s.Labels) != 0 || s.FloatValue != 0 {
			return reject("invalid_sample_set")
		}
		return nil
	}
	if p.ScrapeStatus != "success" || len(p.Samples) != 11 {
		return reject("invalid_sample_set")
	}
	counts := map[string]int{}
	seen := map[string]struct{}{}
	modes := map[string]bool{}
	dbValues := map[string]bool{}
	for i := range p.Samples {
		s := &p.Samples[i]
		rule, ok := successRules[s.Name]
		if !ok {
			return reject("unknown_metric_family")
		}
		if err := validateSample(s, rule); err != nil {
			return err
		}
		key := canonicalKey(*s)
		if _, ok := seen[key]; ok {
			return reject("duplicate_sample")
		}
		seen[key] = struct{}{}
		counts[s.Name]++
		if s.Name == "gopulse_redis_up" && s.FloatValue != 1 {
			return reject("invalid_sample_set")
		}
		if mode, ok := s.Labels["mode"]; ok {
			if mode != "user" && mode != "system" {
				return reject("invalid_label")
			}
			modes[mode] = true
		}
		if db, ok := s.Labels["db"]; ok {
			if _, err := strconv.ParseUint(db, 10, 31); err != nil {
				return reject("invalid_label")
			}
			dbValues[db] = true
		}
	}
	for name, rule := range successRules {
		if counts[name] != rule.count {
			return reject("invalid_sample_set")
		}
	}
	if len(modes) != 2 || !modes["user"] || !modes["system"] || len(dbValues) != 1 {
		return reject("invalid_sample_set")
	}
	return nil
}
func validateSample(s *Sample, rule familyRule) error {
	if len(s.Name) == 0 || len(s.Name) > 128 || s.Kind != rule.kind || s.Labels == nil || len(s.Labels) > 16 {
		return reject("invalid_sample")
	}
	if len(s.Labels) != len(rule.labels) {
		return reject("invalid_label")
	}
	allowed := map[string]bool{}
	for _, name := range rule.labels {
		allowed[name] = true
	}
	for key, value := range s.Labels {
		if !allowed[key] || key == "source" || key == "target_id" || len(value) > 256 {
			return reject("invalid_label")
		}
	}
	value, err := strconv.ParseFloat(string(s.Value), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return reject("invalid_value")
	}
	if rule.counter && value < 0 {
		return reject("invalid_value")
	}
	s.FloatValue = value
	return nil
}
func canonicalKey(s Sample) string {
	keys := make([]string, 0, len(s.Labels))
	for k := range s.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(s.Name)
	for _, k := range keys {
		b.WriteByte(0)
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(s.Labels[k])
	}
	return b.String()
}
func CanonicalKey(s Sample) string { return canonicalKey(s) }
