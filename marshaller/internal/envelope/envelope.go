package envelope

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
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
	source := strings.TrimSuffix(p.PluginID, "-exporter")
	rules := rulesFor(source)
	if rules == nil || !semverPattern.MatchString(p.PluginVersion) || p.TargetID != p.PluginID+"-local" {
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
		if s.Name != "gopulse_"+source+"_up" || len(s.Labels) != 0 || s.FloatValue != 0 {
			return reject("invalid_sample_set")
		}
		return nil
	}
	if p.ScrapeStatus != "success" || len(p.Samples) != sampleCount(rules) {
		return reject("invalid_sample_set")
	}
	counts := map[string]int{}
	healthSum := 0.0
	seen := map[string]struct{}{}
	modes := map[string]bool{}
	dbValues := map[string]bool{}
	for i := range p.Samples {
		s := &p.Samples[i]
		rule, ok := rules[s.Name]
		if !ok {
			return reject("unknown_metric_family")
		}
		if err := validateSample(s, rule); err != nil {
			return err
		}
		if s.Name == "gopulse_elasticsearch_cluster_health_status" {
			if s.FloatValue != 0 && s.FloatValue != 1 {
				return reject("invalid_sample_set")
			}
			healthSum += s.FloatValue
		}
		key := canonicalKey(*s)
		if _, ok := seen[key]; ok {
			return reject("duplicate_sample")
		}
		seen[key] = struct{}{}
		counts[s.Name]++
		if s.Name == "gopulse_"+source+"_up" && s.FloatValue != 1 {
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
	for name, rule := range rules {
		if counts[name] != rule.count {
			return reject("invalid_sample_set")
		}
	}
	if source == "elasticsearch" && healthSum != 1 {
		return reject("invalid_sample_set")
	}
	if source == "redis" && (len(modes) != 2 || !modes["user"] || !modes["system"] || len(dbValues) != 1) {
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
		if key == "status" && value != "green" && value != "yellow" && value != "red" {
			return reject("invalid_label")
		}
		if (key == "result" && value != "commit" && value != "rollback") || (key == "state" && value != "ready" && value != "unacked") {
			return reject("invalid_label")
		}
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

func sampleCount(rules map[string]familyRule) int {
	n := 0
	for _, rule := range rules {
		n += rule.count
	}
	return n
}
func rulesFor(source string) map[string]familyRule {
	switch source {
	case "redis":
		return successRules
	case "mysql":
		return map[string]familyRule{
			"gopulse_mysql_up":                      {kind: "gauge", counter: false, count: 1, labels: nil},
			"gopulse_mysql_uptime_seconds":          {kind: "gauge", counter: false, count: 1, labels: nil},
			"gopulse_mysql_connections":             {kind: "gauge", counter: false, count: 1, labels: nil},
			"gopulse_mysql_max_connections":         {kind: "gauge", counter: false, count: 1, labels: nil},
			"gopulse_mysql_threads_running":         {kind: "gauge", counter: false, count: 1, labels: nil},
			"gopulse_mysql_queries_total":           {kind: "counter", counter: true, count: 1, labels: nil},
			"gopulse_mysql_slow_queries_total":      {kind: "counter", counter: true, count: 1, labels: nil},
			"gopulse_mysql_transactions_total":      {kind: "counter", counter: true, count: 2, labels: []string{"result"}},
			"gopulse_mysql_buffer_pool_data_bytes":  {kind: "gauge", counter: false, count: 1, labels: nil},
			"gopulse_mysql_buffer_pool_dirty_bytes": {kind: "gauge", counter: false, count: 1, labels: nil},
		}
	case "rabbitmq":
		return map[string]familyRule{
			"gopulse_rabbitmq_up":              {kind: "gauge", counter: false, count: 1, labels: nil},
			"gopulse_rabbitmq_connections":     {kind: "gauge", counter: false, count: 1, labels: nil},
			"gopulse_rabbitmq_channels":        {kind: "gauge", counter: false, count: 1, labels: nil},
			"gopulse_rabbitmq_queues":          {kind: "gauge", counter: false, count: 1, labels: nil},
			"gopulse_rabbitmq_consumers":       {kind: "gauge", counter: false, count: 1, labels: nil},
			"gopulse_rabbitmq_messages":        {kind: "gauge", counter: false, count: 2, labels: []string{"state"}},
			"gopulse_rabbitmq_published_total": {kind: "counter", counter: true, count: 1, labels: nil},
			"gopulse_rabbitmq_delivered_total": {kind: "counter", counter: true, count: 1, labels: nil},
			"gopulse_rabbitmq_acked_total":     {kind: "counter", counter: true, count: 1, labels: nil},
		}
	case "kafka":
		return map[string]familyRule{
			"gopulse_kafka_up":                          {kind: "gauge", count: 1, labels: nil},
			"gopulse_kafka_brokers":                     {kind: "gauge", count: 1, labels: nil},
			"gopulse_kafka_controller_available":        {kind: "gauge", count: 1, labels: nil},
			"gopulse_kafka_partitions":                  {kind: "gauge", count: 1, labels: nil},
			"gopulse_kafka_under_replicated_partitions": {kind: "gauge", count: 1, labels: nil},
			"gopulse_kafka_offline_partitions":          {kind: "gauge", count: 1, labels: nil},
			"gopulse_kafka_consumer_group_lag":          {kind: "gauge", count: 1, labels: nil},
		}

	case "victoriametrics":
		return map[string]familyRule{
			"gopulse_victoriametrics_up":                         {kind: "gauge", count: 1, labels: nil},
			"gopulse_victoriametrics_rows_inserted_total":        {kind: "counter", count: 1, labels: nil},
			"gopulse_victoriametrics_query_requests_total":       {kind: "counter", count: 1, labels: nil},
			"gopulse_victoriametrics_active_timeseries":          {kind: "gauge", count: 1, labels: nil},
			"gopulse_victoriametrics_storage_rows":               {kind: "gauge", count: 1, labels: nil},
			"gopulse_victoriametrics_storage_size_bytes":         {kind: "gauge", count: 1, labels: nil},
			"gopulse_victoriametrics_free_disk_space_bytes":      {kind: "gauge", count: 1, labels: nil},
			"gopulse_victoriametrics_active_merges":              {kind: "gauge", count: 1, labels: nil},
			"gopulse_victoriametrics_storage_rows_deleted_total": {kind: "counter", count: 1, labels: nil},
		}
	case "elasticsearch":
		return map[string]familyRule{
			"gopulse_elasticsearch_up":                    {kind: "gauge", count: 1, labels: nil},
			"gopulse_elasticsearch_cluster_health_status": {kind: "gauge", count: 3, labels: []string{"status"}},
			"gopulse_elasticsearch_nodes":                 {kind: "gauge", count: 1, labels: nil},
			"gopulse_elasticsearch_data_nodes":            {kind: "gauge", count: 1, labels: nil},
			"gopulse_elasticsearch_active_primary_shards": {kind: "gauge", count: 1, labels: nil},
			"gopulse_elasticsearch_active_shards":         {kind: "gauge", count: 1, labels: nil},
			"gopulse_elasticsearch_relocating_shards":     {kind: "gauge", count: 1, labels: nil},
			"gopulse_elasticsearch_initializing_shards":   {kind: "gauge", count: 1, labels: nil},
			"gopulse_elasticsearch_unassigned_shards":     {kind: "gauge", count: 1, labels: nil},
			"gopulse_elasticsearch_pending_tasks":         {kind: "gauge", count: 1, labels: nil},
			"gopulse_elasticsearch_documents":             {kind: "gauge", count: 1, labels: nil},
			"gopulse_elasticsearch_store_size_bytes":      {kind: "gauge", count: 1, labels: nil},
		}

	}
	return nil
}

func validProducer(source string, p Payload) bool {
	if componentmetrics.IsComponent(source) {
		return p.ProducerKind == "component" && p.ProducerID == source && p.TargetID == componentmetrics.Target(source)
	}
	return p.ProducerKind == "exporter_plugin" && p.ProducerID == source+"-exporter"
}
func validateMetricsPayload(source string, p *Payload) error {
	if !componentmetrics.IsComponent(source) {
		return validatePayload(p)
	}
	if p.ScrapeStatus != "success" {
		return reject("invalid_component_status")
	}
	samples := make([]componentmetrics.Sample, len(p.Samples))
	for i := range p.Samples {
		s := &p.Samples[i]
		v, err := s.Value.Float64()
		if err != nil {
			return reject("invalid_component_value")
		}
		s.FloatValue = v
		samples[i] = componentmetrics.Sample{Name: s.Name, Kind: s.Kind, Labels: s.Labels, Value: v}
	}
	if err := componentmetrics.Validate(source, samples); err != nil {
		return reject("invalid_component_samples")
	}
	return nil
}
