package collector

import (
	"context"
	"sort"
	"strings"

	"github.com/Ray-ymq/GoPulse/exporters/elasticsearch/common"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

func emitText(ctx context.Context, body string, ch chan<- prometheus.Metric) error {
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(body))
	if err != nil {
		return ErrUnavailable
	}
	names := make([]string, 0, len(families))
	for name := range families {
		names = append(names, name)
	}
	sort.Strings(names)
	ordered := make([]*dto.MetricFamily, 0, len(names))
	for _, name := range names {
		ordered = append(ordered, families[name])
	}
	return emitFamilies(ctx, ordered, ch)
}
func emitFamilies(ctx context.Context, families []*dto.MetricFamily, ch chan<- prometheus.Metric) error {
	foundUp := false
	for _, family := range families {
		if family.GetName() == common.NameSpace+"_up" {
			if foundUp || len(family.Metric) != 1 || family.GetType() != dto.MetricType_GAUGE || len(family.Metric[0].Label) != 0 || family.Metric[0].GetGauge().GetValue() != 1 {
				return ErrUnavailable
			}
			foundUp = true
			continue
		}
		for _, sample := range family.Metric {
			if sample.TimestampMs != nil {
				return ErrUnavailable
			}
			names, values := make([]string, 0, len(sample.Label)), make([]string, 0, len(sample.Label))
			for _, pair := range sample.Label {
				names = append(names, pair.GetName())
				values = append(values, pair.GetValue())
			}
			kind, value := prometheus.GaugeValue, sample.GetGauge().GetValue()
			if family.GetType() == dto.MetricType_COUNTER {
				kind, value = prometheus.CounterValue, sample.GetCounter().GetValue()
			} else if family.GetType() != dto.MetricType_GAUGE {
				return ErrUnavailable
			}
			desc := prometheus.NewDesc(family.GetName(), family.GetHelp(), names, nil)
			metric, err := prometheus.NewConstMetric(desc, kind, value, values...)
			if err != nil {
				return ErrUnavailable
			}
			select {
			case ch <- metric:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	if !foundUp {
		return ErrUnavailable
	}
	return nil
}
