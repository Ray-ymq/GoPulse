package collector

import (
	"bytes"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/envelope"
	"github.com/Ray-ymq/GoPulse/runtimekit/metriccontract"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

func parseExposition(source string, body []byte) ([]envelope.Sample, error) {
	if len(body) > metriccontract.MaxBodyBytes || bytes.ContainsRune(body, 0) {
		return nil, errors.New("contract_invalid")
	}
	// Validate declarations too. Parsers can omit families without samples.
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "#" && (fields[1] == "TYPE" || fields[1] == "HELP") && !metriccontract.ValidName(source, fields[2]) {
			return nil, errors.New("contract_invalid")
		}
	}
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("parse_failed")
	}
	if len(families) > metriccontract.MaxFamilies {
		return nil, errors.New("contract_invalid")
	}
	var samples []envelope.Sample
	for name, family := range families {
		if !metriccontract.ValidName(source, name) || family.Type == nil {
			return nil, errors.New("contract_invalid")
		}
		for _, metric := range family.Metric {
			if metric == nil || metric.TimestampMs != nil {
				return nil, errors.New("contract_invalid")
			}
			labels := make(map[string]string, len(metric.Label))
			for _, pair := range metric.Label {
				if pair == nil {
					return nil, errors.New("contract_invalid")
				}
				if _, exists := labels[pair.GetName()]; exists {
					return nil, errors.New("contract_invalid")
				}
				labels[pair.GetName()] = pair.GetValue()
			}
			if !metriccontract.ValidLabels(labels) {
				return nil, errors.New("contract_invalid")
			}
			add := func(name, kind string, value float64, extraKey, extraValue string) error {
				copyLabels := make(map[string]string, len(labels)+1)
				for k, v := range labels {
					copyLabels[k] = v
				}
				if extraKey != "" {
					if _, exists := copyLabels[extraKey]; exists {
						return errors.New("contract_invalid")
					}
					copyLabels[extraKey] = extraValue
				}
				if len(samples) >= metriccontract.MaxSamples {
					return errors.New("contract_invalid")
				}
				samples = append(samples, envelope.Sample{Name: name, Kind: kind, Labels: copyLabels, Value: value})
				return nil
			}
			switch family.GetType() {
			case dto.MetricType_GAUGE:
				if metric.Gauge == nil || metric.Gauge.Value == nil {
					return nil, errors.New("contract_invalid")
				}
				err = add(name, "gauge", metric.Gauge.GetValue(), "", "")
			case dto.MetricType_COUNTER:
				if metric.Counter == nil || metric.Counter.Value == nil {
					return nil, errors.New("contract_invalid")
				}
				err = add(name, "counter", metric.Counter.GetValue(), "", "")
			case dto.MetricType_UNTYPED:
				if metric.Untyped == nil || metric.Untyped.Value == nil {
					return nil, errors.New("contract_invalid")
				}
				err = add(name, "gauge", metric.Untyped.GetValue(), "", "")
			case dto.MetricType_HISTOGRAM:
				h := metric.Histogram
				if h == nil || h.SampleCount == nil || h.SampleSum == nil {
					return nil, errors.New("contract_invalid")
				}
				hasInf := false
				for _, bucket := range h.Bucket {
					if bucket == nil || bucket.UpperBound == nil || bucket.CumulativeCount == nil {
						return nil, errors.New("contract_invalid")
					}
					hasInf = hasInf || math.IsInf(bucket.GetUpperBound(), 1)
					if err = add(name+"_bucket", "counter", float64(bucket.GetCumulativeCount()), "le", strconv.FormatFloat(bucket.GetUpperBound(), 'g', -1, 64)); err != nil {
						return nil, err
					}
				}
				if !hasInf {
					if err = add(name+"_bucket", "counter", float64(h.GetSampleCount()), "le", "+Inf"); err != nil {
						return nil, err
					}
				}
				if err = add(name+"_count", "counter", float64(h.GetSampleCount()), "", ""); err != nil {
					return nil, err
				}
				err = add(name+"_sum", "gauge", h.GetSampleSum(), "", "")
			case dto.MetricType_SUMMARY:
				s := metric.Summary
				if s == nil || s.SampleCount == nil || s.SampleSum == nil {
					return nil, errors.New("contract_invalid")
				}
				for _, q := range s.Quantile {
					if q == nil || q.Quantile == nil || q.Value == nil || math.IsNaN(q.GetQuantile()) || q.GetQuantile() < 0 || q.GetQuantile() > 1 {
						return nil, errors.New("contract_invalid")
					}
					if err = add(name, "gauge", q.GetValue(), "quantile", strconv.FormatFloat(q.GetQuantile(), 'g', -1, 64)); err != nil {
						return nil, err
					}
				}
				if err = add(name+"_count", "counter", float64(s.GetSampleCount()), "", ""); err != nil {
					return nil, err
				}
				err = add(name+"_sum", "gauge", s.GetSampleSum(), "", "")
			default:
				return nil, errors.New("contract_invalid")
			}
			if err != nil {
				return nil, err
			}
		}
	}
	checked := make([]metriccontract.Sample, len(samples))
	for i, s := range samples {
		checked[i] = metriccontract.Sample{Name: s.Name, Kind: s.Kind, Labels: s.Labels, Value: s.Value}
	}
	if err := metriccontract.Validate(source, checked); err != nil {
		return nil, errors.New("contract_invalid")
	}
	sort.Slice(samples, func(i, j int) bool { return sampleKey(samples[i]) < sampleKey(samples[j]) })
	return samples, nil
}
