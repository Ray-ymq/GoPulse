package metricquery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
)

const (
	maximumBodyBytes = 2 << 20
	maximumSeries    = 32
	maximumPoints    = 4096
)

type Definition struct {
	Metric string `json:"metric"`
	Kind   string `json:"kind"`
	Unit   string `json:"unit"`
	label  string
}

var Catalog = []Definition{
	{Metric: "gopulse_redis_up", Kind: "gauge", Unit: "boolean"},
	{Metric: "gopulse_redis_uptime_seconds", Kind: "gauge", Unit: "seconds"},
	{Metric: "gopulse_redis_connected_clients", Kind: "gauge", Unit: "count"},
	{Metric: "gopulse_redis_used_memory_bytes", Kind: "gauge", Unit: "bytes"},
	{Metric: "gopulse_redis_commands_processed_total", Kind: "counter", Unit: "count"},
	{Metric: "gopulse_redis_keyspace_hits_total", Kind: "counter", Unit: "count"},
	{Metric: "gopulse_redis_keyspace_misses_total", Kind: "counter", Unit: "count"},
	{Metric: "gopulse_redis_cpu_seconds_total", Kind: "counter", Unit: "seconds", label: "mode"},
	{Metric: "gopulse_redis_db_keys", Kind: "gauge", Unit: "count", label: "db"},
	{Metric: "gopulse_redis_db_expiring_keys", Kind: "gauge", Unit: "count", label: "db"},
}

var definitions = func() map[string]Definition {
	result := make(map[string]Definition, len(Catalog))
	for _, definition := range Catalog {
		result[definition.Metric] = definition
	}
	return result
}()

type Range struct {
	Name     string
	Duration time.Duration
	Step     time.Duration
}

var ranges = map[string]Range{
	"15m": {Name: "15m", Duration: 15 * time.Minute, Step: 15 * time.Second},
	"1h":  {Name: "1h", Duration: time.Hour, Step: time.Minute},
	"6h":  {Name: "6h", Duration: 6 * time.Hour, Step: 5 * time.Minute},
	"24h": {Name: "24h", Duration: 24 * time.Hour, Step: 15 * time.Minute},
}

type Options struct {
	Definition Definition
	Range      Range
}

type Labels struct {
	Mode string `json:"mode,omitempty"`
	DB   string `json:"db,omitempty"`
}

type Point struct {
	Timestamp string  `json:"timestamp"`
	Value     float64 `json:"value"`
}

type Series struct {
	Labels Labels  `json:"labels"`
	Points []Point `json:"points"`
}

type Result struct {
	Metric      string   `json:"metric"`
	Kind        string   `json:"kind"`
	Unit        string   `json:"unit"`
	Range       string   `json:"range"`
	From        string   `json:"from"`
	To          string   `json:"to"`
	StepSeconds int64    `json:"step_seconds"`
	Series      []Series `json:"series"`
}

func ParseOptions(values url.Values) (Options, error) {
	if len(values) < 1 || len(values) > 2 {
		return Options{}, validation()
	}
	for key, entries := range values {
		if key != "metric" && key != "range" || len(entries) != 1 || entries[0] == "" || len(entries[0]) > 128 || hasControl(entries[0]) {
			return Options{}, validation()
		}
	}
	metricValues, ok := values["metric"]
	if !ok || len(metricValues) != 1 {
		return Options{}, validation()
	}
	definition, ok := definitions[metricValues[0]]
	if !ok {
		return Options{}, validation()
	}
	rangeName := "15m"
	if entries, ok := values["range"]; ok {
		if len(entries) != 1 {
			return Options{}, validation()
		}
		rangeName = entries[0]
	}
	rangeValue, ok := ranges[rangeName]
	if !ok {
		return Options{}, validation()
	}
	return Options{Definition: definition, Range: rangeValue}, nil
}

func QueryExpression(metric string) string {
	return metric + `{source="redis",target_id="redis-exporter-local"}`
}

type Upstream interface {
	QueryRange(context.Context, string, time.Time, time.Time, time.Duration) ([]byte, error)
}

type Service struct {
	upstream Upstream
	now      func() time.Time
}

func NewService(upstream Upstream) *Service { return &Service{upstream: upstream, now: time.Now} }

func (s *Service) Query(ctx context.Context, options Options) (Result, error) {
	to := s.now().UTC()
	from := to.Add(-options.Range.Duration)
	body, err := s.upstream.QueryRange(ctx, QueryExpression(options.Definition.Metric), from, to, options.Range.Step)
	if err != nil {
		return Result{}, unavailable()
	}
	series, err := decodeResponse(body, options.Definition)
	if err != nil {
		return Result{}, unavailable()
	}
	return Result{
		Metric: options.Definition.Metric, Kind: options.Definition.Kind, Unit: options.Definition.Unit,
		Range: options.Range.Name, From: formatTime(from), To: formatTime(to),
		StepSeconds: int64(options.Range.Step / time.Second), Series: series,
	}, nil
}

type Client struct {
	endpoint string
	username string
	password string
	client   *http.Client
}

func NewClient(baseURL, username, password string, timeout time.Duration) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("invalid VictoriaMetrics URL")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxResponseHeaderBytes = 64 << 10
	return &Client{
		endpoint: strings.TrimRight(baseURL, "/") + "/prometheus/api/v1/query_range",
		username: username, password: password,
		client: &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

func (c *Client) QueryRange(ctx context.Context, query string, from, to time.Time, step time.Duration) ([]byte, error) {
	form := url.Values{}
	form.Set("query", query)
	form.Set("start", formatTime(from))
	form.Set("end", formatTime(to))
	form.Set("step", strconv.FormatInt(int64(step/time.Second), 10)+"s")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.SetBasicAuth(c.username, c.password)
	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, errors.New("VictoriaMetrics request failed")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumBodyBytes+1))
	if err != nil || len(body) > maximumBodyBytes {
		return nil, errors.New("VictoriaMetrics response unavailable")
	}
	return body, nil
}

type upstreamEnvelope struct {
	Status string         `json:"status"`
	Data   upstreamData   `json:"data"`
	Stats  *upstreamStats `json:"stats,omitempty"`
}
type upstreamStats struct {
	SeriesFetched     string `json:"seriesFetched"`
	ExecutionTimeMsec int64  `json:"executionTimeMsec"`
}
type upstreamData struct {
	ResultType string           `json:"resultType"`
	Result     []upstreamSeries `json:"result"`
	resultSeen bool
}

func (d *upstreamData) UnmarshalJSON(data []byte) error {
	type alias upstreamData
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || len(raw) != 2 || raw["resultType"] == nil || raw["result"] == nil {
		return errors.New("invalid data object")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var value alias
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	*d = upstreamData(value)
	d.resultSeen = true
	return nil
}

type upstreamSeries struct {
	Metric map[string]string   `json:"metric"`
	Values [][]json.RawMessage `json:"values"`
}

func decodeResponse(body []byte, definition Definition) ([]Series, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var envelope upstreamEnvelope
	if err := decoder.Decode(&envelope); err != nil || envelope.Status != "success" || envelope.Data.ResultType != "matrix" || !envelope.Data.resultSeen {
		return nil, errors.New("invalid VictoriaMetrics response")
	}
	if err := ensureEOF(decoder); err != nil || len(envelope.Data.Result) > maximumSeries {
		return nil, errors.New("invalid VictoriaMetrics response")
	}
	result := make([]Series, 0, len(envelope.Data.Result))
	seen := map[string]struct{}{}
	totalPoints := 0
	for _, raw := range envelope.Data.Result {
		labels, key, err := validateLabels(raw.Metric, definition)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[key]; exists {
			return nil, errors.New("duplicate series")
		}
		seen[key] = struct{}{}
		points := make([]Point, 0, len(raw.Values))
		var previous time.Time
		for _, pair := range raw.Values {
			if len(pair) != 2 {
				return nil, errors.New("invalid point")
			}
			timestamp, err := decodeTimestamp(pair[0])
			if err != nil || (!previous.IsZero() && !timestamp.After(previous)) {
				return nil, errors.New("invalid point timestamp")
			}
			previous = timestamp
			value, err := decodeValue(pair[1])
			if err != nil {
				return nil, err
			}
			points = append(points, Point{Timestamp: formatTime(timestamp), Value: value})
			totalPoints++
			if totalPoints > maximumPoints {
				return nil, errors.New("too many points")
			}
		}
		result = append(result, Series{Labels: labels, Points: points})
	}
	sort.Slice(result, func(i, j int) bool { return labelKey(result[i].Labels) < labelKey(result[j].Labels) })
	return result, nil
}

func validateLabels(metric map[string]string, definition Definition) (Labels, string, error) {
	if metric["__name__"] != definition.Metric || metric["source"] != "redis" || metric["target_id"] != "redis-exporter-local" {
		return Labels{}, "", errors.New("invalid metric provenance")
	}
	if len(metric) != 3 && !(definition.label != "" && len(metric) == 4) {
		return Labels{}, "", errors.New("unknown metric label")
	}
	labels := Labels{}
	switch definition.label {
	case "":
	case "mode":
		if metric["mode"] != "user" && metric["mode"] != "system" {
			return Labels{}, "", errors.New("invalid mode label")
		}
		labels.Mode = metric["mode"]
	case "db":
		value := metric["db"]
		parsed, err := strconv.ParseUint(value, 10, 32)
		if err != nil || strconv.FormatUint(parsed, 10) != value {
			return Labels{}, "", errors.New("invalid db label")
		}
		labels.DB = value
	default:
		return Labels{}, "", errors.New("invalid catalog")
	}
	return labels, labelKey(labels), nil
}

func labelKey(labels Labels) string { return labels.Mode + "\x00" + labels.DB }

func decodeTimestamp(raw json.RawMessage) (time.Time, error) {
	var seconds float64
	if err := json.Unmarshal(raw, &seconds); err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 {
		return time.Time{}, errors.New("invalid timestamp")
	}
	whole, fraction := math.Modf(seconds)
	return time.Unix(int64(whole), int64(math.Round(fraction*1e9))).UTC(), nil
}

func decodeValue(raw json.RawMessage) (float64, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil || text == "" {
		return 0, errors.New("invalid value")
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, errors.New("invalid value")
	}
	return value, nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("trailing response data")
	}
	return nil
}
func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
func hasControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}
func validation() error {
	return apperror.New(apperror.CodeValidationFailed, "metric query parameters are invalid")
}
func unavailable() error {
	return apperror.New(apperror.CodeMetricsUnavailable, "metrics are temporarily unavailable")
}
