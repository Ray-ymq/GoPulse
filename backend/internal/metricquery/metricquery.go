package metricquery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"github.com/Ray-ymq/GoPulse/componentmetrics/metriccontract"
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
	maximumBodyBytes          = 2 << 20
	maximumSeries             = 1024
	maximumPoints             = maximumSeries * 97
	maximumSelectorCount      = 8
	maximumSelectorValueBytes = 256
)

type Definition struct {
	Source         string   `json:"source"`
	TargetID       string   `json:"target_id"`
	ProducerKind   string   `json:"producer_kind"`
	ProducerID     string   `json:"producer_id"`
	Metric         string   `json:"metric"`
	Kind           string   `json:"kind"`
	Unit           string   `json:"unit"`
	LabelKeys      []string `json:"label_keys,omitempty"`
	ValueKind      string   `json:"value_kind,omitempty"`
	DisplayUnit    string   `json:"display_unit,omitempty"`
	label          string
	labelKeysKnown bool
	derived        *derivedPlan
}

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
	Labels     Labels
}

type Labels map[string]string

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
	ValueKind   string   `json:"value_kind,omitempty"`
	DisplayUnit string   `json:"display_unit,omitempty"`
	Quality     string   `json:"quality,omitempty"`
	SampledAt   string   `json:"sampled_at,omitempty"`
	Range       string   `json:"range"`
	From        string   `json:"from"`
	To          string   `json:"to"`
	StepSeconds int64    `json:"step_seconds"`
	Series      []Series `json:"series"`
}

func ParseOptions(values url.Values) (Options, error) {
	if len(values) < 1 || len(values) > maximumSelectorCount+2 {
		return Options{}, validation()
	}
	labels := Labels{}
	for key, entries := range values {
		if len(entries) != 1 || entries[0] == "" || len(entries[0]) > maximumSelectorValueBytes || hasControl(entries[0]) {
			return Options{}, validation()
		}
		if key != "metric" && key != "range" {
			if !validSelectorKey(key) || len(labels) >= maximumSelectorCount {
				return Options{}, validation()
			}
			labels[key] = entries[0]
		}
	}
	metricValues, ok := values["metric"]
	if !ok || len(metricValues) != 1 {
		return Options{}, validation()
	}
	definition, ok := definitionFor(metricValues[0])
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
	return Options{Definition: definition, Range: rangeValue, Labels: labels}, nil
}

func QueryExpression(metric string) string {
	return queryExpression(metric, nil)
}

func queryExpression(metric string, labels Labels) string {
	d, ok := definitionFor(metric)
	if !ok || d.derived != nil {
		return ""
	}
	selectors := make([]string, 0, len(labels))
	for key, value := range labels {
		selectors = append(selectors, key+"="+strconv.Quote(value))
	}
	sort.Strings(selectors)
	identity := []string{`source="` + d.Source + `"`, `target_id="` + d.TargetID + `"`}
	if d.Source != "redis" {
		identity = append(identity, `producer_kind="`+d.ProducerKind+`"`, `producer_id="`+d.ProducerID+`"`)
	}
	selectors = append(identity, selectors...)
	return metric + "{" + strings.Join(selectors, ",") + "}"
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
	if options.Definition.derived != nil {
		result, err := s.queryDerived(ctx, options, from, to)
		if err != nil {
			return Result{}, unavailable()
		}
		return result, nil
	}
	definition, err := s.resolveDefinition(ctx, options.Definition, from, to)
	if err != nil || definition.Kind == "" {
		return Result{}, unavailable()
	}
	if !validSelectors(options.Labels, definition) {
		return Result{}, validation()
	}
	body, err := s.upstream.QueryRange(ctx, queryExpression(definition.Metric, options.Labels), from, to, options.Range.Step)
	if err != nil {
		return Result{}, unavailable()
	}
	series, err := decodeResponse(body, definition)
	if err != nil {
		return Result{}, unavailable()
	}
	return newResult(definition, options.Range, from, to, series), nil
}

func (s *Service) resolveDefinition(ctx context.Context, definition Definition, from, to time.Time) (Definition, error) {
	if definition.derived != nil {
		return definition, nil
	}
	discovery, ok := s.upstream.(MetricDiscovery)
	if !ok {
		return definition, nil
	}
	entries, err := discovery.DiscoverMetrics(ctx, definition.Metric, to.Add(-24*time.Hour), to)
	if err != nil {
		return Definition{}, err
	}
	if len(entries) > 1 {
		return Definition{}, errors.New("metric has multiple definitions")
	}
	if len(entries) == 1 {
		return entries[0], nil
	}
	return definition, nil
}

func newResult(definition Definition, queryRange Range, from, to time.Time, series []Series) Result {
	result := Result{
		Metric: definition.Metric, Kind: definition.Kind, Unit: definition.Unit,
		ValueKind: definition.ValueKind, DisplayUnit: definition.DisplayUnit,
		Range: queryRange.Name, From: formatTime(from), To: formatTime(to),
		StepSeconds: int64(queryRange.Step / time.Second), Series: series,
	}
	var sampledAt time.Time
	for _, row := range series {
		if len(row.Points) == 0 {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, row.Points[len(row.Points)-1].Timestamp)
		if err == nil && at.After(sampledAt) {
			sampledAt = at
		}
	}
	if !sampledAt.IsZero() {
		result.SampledAt = formatTime(sampledAt)
	}
	return result
}

func validSelectors(labels Labels, definition Definition) bool {
	allowed := make(map[string]bool, len(definition.LabelKeys))
	for _, key := range definition.LabelKeys {
		allowed[key] = true
	}
	for key := range labels {
		if !validSelectorKey(key) || definition.labelKeysKnown && !allowed[key] {
			return false
		}
	}
	return true
}

func validSelectorKey(key string) bool {
	if key == "" || len(key) > metriccontract.MaxLabelNameBytes || strings.HasPrefix(key, "__") {
		return false
	}
	for index, character := range key {
		letter := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
		digit := character >= '0' && character <= '9'
		if !letter && character != '_' && (!digit || index == 0) {
			return false
		}
		if index == 0 && character >= '0' && character <= '9' {
			return false
		}
	}
	return key != "source" && key != "target_id" && key != "producer_kind" && key != "producer_id" && key != "producer_version" && key != "metric" && key != "range"
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
		client: &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

func (c *Client) QueryRange(ctx context.Context, query string, from, to time.Time, step time.Duration) ([]byte, error) {
	form := url.Values{}
	form.Set("query", query)
	form.Set("start", formatTime(from))
	form.Set("end", formatTime(to))
	form.Set("step", strconv.FormatInt(int64(step/time.Second), 10)+"s")
	request, err := componentmetrics.NewRequest(ctx, http.MethodPost, c.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.SetBasicAuth(c.username, c.password)
	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
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
	seriesLimit, pointsLimit := maximumSeries, maximumPoints
	if err := ensureEOF(decoder); err != nil || len(envelope.Data.Result) > seriesLimit {
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
			if err := metriccontract.ValidateSample(definition.Source, metriccontract.Sample{Name: definition.Metric, Kind: definition.Kind, Labels: labels, Value: value}); err != nil {
				return nil, err
			}
			if (definition.Kind == "counter" && value < 0) || (definition.Unit == "state" && value != -1 && value != 0 && value != 1) || (definition.Unit == "boolean" && value != 0 && value != 1) {
				return nil, errors.New("invalid metric value")
			}
			points = append(points, Point{Timestamp: formatTime(timestamp), Value: value})
			totalPoints++
			if totalPoints > pointsLimit {
				return nil, errors.New("too many points")
			}
		}
		result = append(result, Series{Labels: labels, Points: points})
	}
	sort.Slice(result, func(i, j int) bool { return labelKey(result[i].Labels) < labelKey(result[j].Labels) })
	return result, nil
}

func validateLabels(metric map[string]string, definition Definition) (Labels, string, error) {
	if metric["__name__"] != definition.Metric || metric["source"] != definition.Source || metric["target_id"] != definition.TargetID {
		return nil, "", errors.New("invalid metric provenance")
	}
	if definition.Source == "redis" {
		if _, ok := metric["producer_kind"]; ok {
			return nil, "", errors.New("invalid metric provenance")
		}
		if _, ok := metric["producer_id"]; ok {
			return nil, "", errors.New("invalid metric provenance")
		}
	} else if metric["producer_kind"] != definition.ProducerKind || metric["producer_id"] != definition.ProducerID {
		return nil, "", errors.New("invalid metric provenance")
	}
	labels := Labels(componentLabelMap(metric))
	if !metriccontract.ValidLabels(labels) {
		return nil, "", errors.New("invalid metric labels")
	}
	return labels, labelKey(labels), nil
}

func labelKey(labels Labels) string {
	body, _ := json.Marshal(labels)
	return string(body)
}

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

func componentLabelMap(metric map[string]string) map[string]string {
	labels := make(map[string]string)
	for k, v := range metric {
		switch k {
		case "__name__", "source", "target_id", "producer_kind", "producer_id":
		default:
			labels[k] = v
		}
	}
	return labels
}
