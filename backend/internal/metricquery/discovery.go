package metricquery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"github.com/Ray-ymq/GoPulse/componentmetrics/metriccontract"
)

const maximumDefinitions = 2048

type MetricDiscovery interface {
	DiscoverMetrics(context.Context, string, time.Time, time.Time) ([]Definition, error)
}

func definitionFor(metric string) (Definition, bool) {
	if derived, ok := derivedDefinition(metric); ok {
		return derived, true
	}
	for _, source := range append(append([]string{}, componentmetrics.Plugins...), componentmetrics.Components...) {
		if !metriccontract.ValidName(source, metric) {
			continue
		}
		d := Definition{Source: source, TargetID: source + "-exporter-local", ProducerKind: "exporter_plugin", ProducerID: source + "-exporter", Metric: metric, Unit: "none"}
		if componentmetrics.IsComponent(source) {
			d.TargetID, d.ProducerKind, d.ProducerID = componentmetrics.Target(source), "component", source
		}
		if preset, ok := presets[metric]; ok {
			d.Kind, d.Unit = preset.Kind, preset.Unit
			d.LabelKeys = append([]string(nil), preset.LabelKeys...)
			d.labelKeysKnown = preset.labelKeysKnown
		}
		return d, true
	}
	return Definition{}, false
}

func (s *Service) Catalog(ctx context.Context) ([]Definition, error) {
	discovery, ok := s.upstream.(MetricDiscovery)
	if !ok {
		return nil, unavailable()
	}
	to := s.now().UTC()
	entries, err := discovery.DiscoverMetrics(ctx, "", to.Add(-24*time.Hour), to)
	if err != nil {
		return nil, unavailable()
	}
	// Presets keep historical metrics visible during a rolling upgrade.
	byName := make(map[string]Definition, len(Presets)+len(entries))
	for _, d := range Presets {
		byName[d.Metric] = d
	}
	for _, d := range derivedCatalog() {
		byName[d.Metric] = d
	}
	for _, d := range entries {
		byName[d.Metric] = d
	}
	if len(byName) > maximumDefinitions {
		return nil, unavailable()
	}
	result := make([]Definition, 0, len(byName))
	for _, d := range byName {
		result = append(result, d)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Metric < result[j].Metric })
	return result, nil
}

func (c *Client) DiscoverMetrics(ctx context.Context, metric string, from, to time.Time) ([]Definition, error) {
	selector := metriccontract.MetadataName
	if metric != "" {
		if _, ok := definitionFor(metric); !ok {
			return nil, errors.New("invalid metric name")
		}
		selector += `{metric=` + strconv.Quote(metric) + `}`
	}
	form := url.Values{"match[]": {selector}, "start": {formatTime(from)}, "end": {formatTime(to)}, "limit": {strconv.Itoa(maximumDefinitions + 1)}}
	request, err := componentmetrics.NewRequest(ctx, http.MethodPost, strings.TrimSuffix(c.endpoint, "/query_range")+"/series", strings.NewReader(form.Encode()))
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
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("metric discovery unavailable")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumBodyBytes+1))
	if err != nil || len(body) > maximumBodyBytes {
		return nil, errors.New("metric discovery response exceeds limit")
	}
	return decodeDefinitions(body, metric)
}

func decodeDefinitions(body []byte, requested string) ([]Definition, error) {
	var response struct {
		Status string              `json:"status"`
		Data   []map[string]string `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil || response.Status != "success" || response.Data == nil || len(response.Data) > maximumDefinitions || ensureEOF(decoder) != nil {
		return nil, errors.New("invalid metric discovery response")
	}
	byName := map[string]Definition{}
	for _, row := range response.Data {
		// VictoriaMetrics drops empty-valued labels; absent label_keys means no business dimensions.
		expectedFields := 8
		if _, ok := row["label_keys"]; ok {
			expectedFields++
		}
		d, ok := definitionFor(row["metric"])
		if !ok || len(row) != expectedFields || row["__name__"] != metriccontract.MetadataName || row["source"] != d.Source || row["target_id"] != d.TargetID || row["producer_kind"] != d.ProducerKind || row["producer_id"] != d.ProducerID || !metriccontract.ValidKind(row["kind"]) || (requested != "" && row["metric"] != requested) {
			return nil, errors.New("invalid metric descriptor")
		}
		switch row["unit"] {
		case "none", "boolean", "seconds", "bytes", "count", "state", "unix_seconds":
		default:
			return nil, errors.New("invalid metric unit")
		}
		d.Kind = row["kind"]
		if _, known := presets[d.Metric]; !known {
			d.Unit = row["unit"]
		}
		keys := map[string]string{}
		if row["label_keys"] != "" {
			for _, key := range strings.Split(row["label_keys"], ",") {
				if _, exists := keys[key]; exists {
					return nil, errors.New("duplicate metric label key")
				}
				keys[key] = ""
			}
		}
		if !metriccontract.ValidLabels(keys) {
			return nil, errors.New("invalid metric label keys")
		}
		if previous, ok := byName[d.Metric]; ok {
			if previous.Kind != d.Kind || previous.Unit != d.Unit {
				return nil, errors.New("conflicting metric descriptors")
			}
			for _, key := range previous.LabelKeys {
				keys[key] = ""
			}
			if !metriccontract.ValidLabels(keys) {
				return nil, errors.New("too many metric label keys")
			}
		}
		for key := range keys {
			d.LabelKeys = append(d.LabelKeys, key)
		}
		sort.Strings(d.LabelKeys)
		d.labelKeysKnown = true
		byName[d.Metric] = d
	}
	result := make([]Definition, 0, len(byName))
	for _, d := range byName {
		result = append(result, d)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Metric < result[j].Metric })
	return result, nil
}
