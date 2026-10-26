package metricquery

import (
	"context"
	"errors"
	"time"
)

const (
	derivedRate    = "rate"
	derivedRatio   = "ratio"
	derivedAverage = "average"
)

// derivedPlan describes a small, bounded calculation over already stored
// series. It intentionally contains no free-form PromQL: every source metric,
// fixed selector and output label is code-owned.
type derivedPlan struct {
	Operation      string
	Sources        []string
	FixedSelectors map[string]string
	DropLabels     []string
}

type derivedSpec struct {
	Name         string
	Source       string
	TargetID     string
	ProducerKind string
	ProducerID   string
	Unit         string
	DisplayUnit  string
	ValueKind    string
	LabelKeys    []string
	Plan         derivedPlan
}

func derivedCatalog() []Definition {
	definitions := make([]Definition, 0, len(derivedSpecs))
	for _, spec := range derivedSpecs {
		definitions = append(definitions, Definition{
			Source: spec.Source, TargetID: spec.TargetID, ProducerKind: spec.ProducerKind,
			ProducerID: spec.ProducerID, Metric: spec.Name, Kind: "gauge", Unit: spec.Unit,
			ValueKind: spec.ValueKind, DisplayUnit: spec.DisplayUnit,
			LabelKeys: append([]string(nil), spec.LabelKeys...), labelKeysKnown: true,
			derived: &derivedPlan{Operation: spec.Plan.Operation, Sources: append([]string(nil), spec.Plan.Sources...), FixedSelectors: copyLabels(spec.Plan.FixedSelectors), DropLabels: append([]string(nil), spec.Plan.DropLabels...)},
		})
	}
	return definitions
}

var derivedByName = func() map[string]Definition {
	result := make(map[string]Definition, len(derivedSpecs))
	for _, definition := range derivedCatalog() {
		result[definition.Metric] = definition
	}
	return result
}()

// The physical inputs all exist in the first exporter batch. The derived
// catalog is deliberately small: one rate, ratio or average has a clear
// denominator and never guesses at a business request count.
var derivedSpecs = []derivedSpec{
	{
		Name: "gopulse_redis_commands_qps", Source: "redis", TargetID: "redis-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "redis-exporter",
		Unit: "count", DisplayUnit: "ops/s", ValueKind: derivedRate,
		Plan: derivedPlan{Operation: derivedRate, Sources: []string{"gopulse_redis_commands_processed_total"}},
	},
	{
		Name: "gopulse_redis_command_qps", Source: "redis", TargetID: "redis-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "redis-exporter",
		Unit: "count", DisplayUnit: "ops/s", ValueKind: derivedRate, LabelKeys: []string{"command"},
		Plan: derivedPlan{Operation: derivedRate, Sources: []string{"gopulse_redis_command_calls_total"}},
	},
	{
		Name: "gopulse_redis_keyspace_hit_rate", Source: "redis", TargetID: "redis-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "redis-exporter",
		Unit: "none", DisplayUnit: "%", ValueKind: derivedRatio,
		Plan: derivedPlan{Operation: derivedRatio, Sources: []string{"gopulse_redis_keyspace_hits_total", "gopulse_redis_keyspace_misses_total"}},
	},
	{
		Name: "gopulse_redis_connection_usage_ratio", Source: "redis", TargetID: "redis-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "redis-exporter",
		Unit: "none", DisplayUnit: "%", ValueKind: derivedRatio,
		Plan: derivedPlan{Operation: derivedRatio, Sources: []string{"gopulse_redis_connected_clients", "gopulse_redis_max_clients"}},
	},
	{
		Name: "gopulse_redis_memory_usage_ratio", Source: "redis", TargetID: "redis-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "redis-exporter",
		Unit: "none", DisplayUnit: "%", ValueKind: derivedRatio,
		Plan: derivedPlan{Operation: derivedRatio, Sources: []string{"gopulse_redis_used_memory_bytes", "gopulse_redis_maxmemory_bytes"}},
	},
	{
		Name: "gopulse_redis_command_average_duration_seconds", Source: "redis", TargetID: "redis-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "redis-exporter",
		Unit: "seconds", DisplayUnit: "s/op", ValueKind: derivedAverage, LabelKeys: []string{"command"},
		Plan: derivedPlan{Operation: derivedAverage, Sources: []string{"gopulse_redis_command_duration_seconds_total", "gopulse_redis_command_calls_total"}},
	},
	{
		Name: "gopulse_mysql_queries_qps", Source: "mysql", TargetID: "mysql-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "mysql-exporter",
		Unit: "count", DisplayUnit: "ops/s", ValueKind: derivedRate,
		Plan: derivedPlan{Operation: derivedRate, Sources: []string{"gopulse_mysql_queries_total"}},
	},
	{
		Name: "gopulse_mysql_statement_qps", Source: "mysql", TargetID: "mysql-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "mysql-exporter",
		Unit: "count", DisplayUnit: "ops/s", ValueKind: derivedRate, LabelKeys: []string{"operation"},
		Plan: derivedPlan{Operation: derivedRate, Sources: []string{"gopulse_mysql_statements_total"}},
	},
	{
		Name: "gopulse_mysql_transaction_qps", Source: "mysql", TargetID: "mysql-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "mysql-exporter",
		Unit: "count", DisplayUnit: "tx/s", ValueKind: derivedRate, LabelKeys: []string{"result"},
		Plan: derivedPlan{Operation: derivedRate, Sources: []string{"gopulse_mysql_transactions_total"}},
	},
	{
		Name: "gopulse_mysql_connection_usage_ratio", Source: "mysql", TargetID: "mysql-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "mysql-exporter",
		Unit: "none", DisplayUnit: "%", ValueKind: derivedRatio,
		Plan: derivedPlan{Operation: derivedRatio, Sources: []string{"gopulse_mysql_connections", "gopulse_mysql_max_connections"}},
	},
	{
		Name: "gopulse_rabbitmq_publish_qps", Source: "rabbitmq", TargetID: "rabbitmq-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "rabbitmq-exporter",
		Unit: "count", DisplayUnit: "msg/s", ValueKind: derivedRate,
		Plan: derivedPlan{Operation: derivedRate, Sources: []string{"gopulse_rabbitmq_published_total"}},
	},
	{
		Name: "gopulse_rabbitmq_deliver_qps", Source: "rabbitmq", TargetID: "rabbitmq-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "rabbitmq-exporter",
		Unit: "count", DisplayUnit: "msg/s", ValueKind: derivedRate,
		Plan: derivedPlan{Operation: derivedRate, Sources: []string{"gopulse_rabbitmq_delivered_total"}},
	},
	{
		Name: "gopulse_rabbitmq_ack_qps", Source: "rabbitmq", TargetID: "rabbitmq-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "rabbitmq-exporter",
		Unit: "count", DisplayUnit: "msg/s", ValueKind: derivedRate,
		Plan: derivedPlan{Operation: derivedRate, Sources: []string{"gopulse_rabbitmq_acked_total"}},
	},
	{
		Name: "gopulse_rabbitmq_queue_publish_qps", Source: "rabbitmq", TargetID: "rabbitmq-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "rabbitmq-exporter",
		Unit: "count", DisplayUnit: "msg/s", ValueKind: derivedRate, LabelKeys: []string{"vhost", "queue"},
		Plan: derivedPlan{Operation: derivedRate, Sources: []string{"gopulse_rabbitmq_queue_messages_total"}, FixedSelectors: map[string]string{"operation": "publish"}, DropLabels: []string{"operation"}},
	},
	{
		Name: "gopulse_rabbitmq_queue_deliver_qps", Source: "rabbitmq", TargetID: "rabbitmq-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "rabbitmq-exporter",
		Unit: "count", DisplayUnit: "msg/s", ValueKind: derivedRate, LabelKeys: []string{"vhost", "queue"},
		Plan: derivedPlan{Operation: derivedRate, Sources: []string{"gopulse_rabbitmq_queue_messages_total"}, FixedSelectors: map[string]string{"operation": "deliver"}, DropLabels: []string{"operation"}},
	},
	{
		Name: "gopulse_elasticsearch_search_average_duration_seconds", Source: "elasticsearch", TargetID: "elasticsearch-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "elasticsearch-exporter",
		Unit: "seconds", DisplayUnit: "s/op", ValueKind: derivedAverage, LabelKeys: []string{"node_id", "operation"},
		Plan: derivedPlan{Operation: derivedAverage, Sources: []string{"gopulse_elasticsearch_search_duration_seconds_total", "gopulse_elasticsearch_search_operations_total"}},
	},
	{
		Name: "gopulse_elasticsearch_indexing_average_duration_seconds", Source: "elasticsearch", TargetID: "elasticsearch-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "elasticsearch-exporter",
		Unit: "seconds", DisplayUnit: "s/op", ValueKind: derivedAverage, LabelKeys: []string{"node_id"},
		Plan: derivedPlan{Operation: derivedAverage, Sources: []string{"gopulse_elasticsearch_indexing_duration_seconds_total", "gopulse_elasticsearch_indexing_operations_total"}},
	},
	{
		Name: "gopulse_victoriametrics_query_qps", Source: "victoriametrics", TargetID: "victoriametrics-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "victoriametrics-exporter",
		Unit: "count", DisplayUnit: "queries/s", ValueKind: derivedRate,
		Plan: derivedPlan{Operation: derivedRate, Sources: []string{"gopulse_victoriametrics_query_requests_total"}},
	},
}

func derivedDefinition(metric string) (Definition, bool) {
	definition, ok := derivedByName[metric]
	return definition, ok
}

func (s *Service) queryDerived(ctx context.Context, options Options, from, to time.Time) (Result, error) {
	plan := options.Definition.derived
	if plan == nil || !validSelectors(options.Labels, options.Definition) {
		return Result{}, errors.New("invalid derived metric selectors")
	}
	inputs := make([][]Series, len(plan.Sources))
	for i, source := range plan.Sources {
		selectors := copyLabels(options.Labels)
		for key, value := range plan.FixedSelectors {
			if old, exists := selectors[key]; exists && old != value {
				return Result{}, errors.New("conflicting derived selector")
			}
			selectors[key] = value
		}
		definition, err := s.resolveDefinition(ctx, mustDefinition(source), from, to)
		if err != nil || definition.Kind == "" || !validSelectors(selectors, definition) {
			return Result{}, errors.New("derived metric source unavailable")
		}
		body, err := s.upstream.QueryRange(ctx, queryExpression(definition.Metric, selectors), from, to, options.Range.Step)
		if err != nil {
			return Result{}, err
		}
		inputs[i], err = decodeResponse(body, definition)
		if err != nil {
			return Result{}, err
		}
		if len(plan.DropLabels) > 0 {
			inputs[i] = stripSeriesLabels(inputs[i], plan.DropLabels)
		}
	}
	series, quality := calculateDerived(*plan, inputs)
	result := newResult(options.Definition, options.Range, from, to, series)
	result.Quality = quality
	return result, nil
}

func mustDefinition(metric string) Definition {
	definition, _ := definitionFor(metric)
	return definition
}

func calculateDerived(plan derivedPlan, inputs [][]Series) ([]Series, string) {
	if len(inputs) == 0 {
		return nil, "sampling"
	}
	switch plan.Operation {
	case derivedRate:
		return deriveRate(inputs[0])
	case derivedRatio:
		return deriveRatio(inputs[0], inputs[1:])
	case derivedAverage:
		if len(inputs) < 2 {
			return nil, "sampling"
		}
		return deriveAverage(inputs[0], inputs[1])
	default:
		return nil, "sampling"
	}
}

func deriveRate(input []Series) ([]Series, string) {
	result := make([]Series, 0, len(input))
	quality := "complete"
	for _, series := range input {
		points := make([]Point, 0, len(series.Points)-1)
		for i := 1; i < len(series.Points); i++ {
			previous, current := series.Points[i-1], series.Points[i]
			elapsed := secondsBetween(previous.Timestamp, current.Timestamp)
			if elapsed <= 0 {
				continue
			}
			delta := current.Value - previous.Value
			if delta < 0 { // counter reset: use the post-reset value as the increment
				delta = current.Value
			}
			points = append(points, Point{Timestamp: current.Timestamp, Value: delta / elapsed})
		}
		if len(points) == 0 {
			quality = "sampling"
		}
		result = append(result, Series{Labels: copyLabels(series.Labels), Points: points})
	}
	return result, quality
}

func deriveRatio(numerator []Series, denominators [][]Series) ([]Series, string) {
	denominator := make(map[string]map[string]float64)
	for _, input := range denominators {
		for _, series := range input {
			key := labelKey(series.Labels)
			if denominator[key] == nil {
				denominator[key] = map[string]float64{}
			}
			for _, point := range series.Points {
				denominator[key][point.Timestamp] += point.Value
			}
		}
	}
	result := make([]Series, 0, len(numerator))
	quality := "complete"
	for _, series := range numerator {
		values := denominator[labelKey(series.Labels)]
		points := make([]Point, 0, len(series.Points))
		for _, point := range series.Points {
			base, ok := values[point.Timestamp]
			if !ok || base == 0 {
				quality = "zero_denominator"
				continue
			}
			points = append(points, Point{Timestamp: point.Timestamp, Value: point.Value / base * 100})
		}
		if len(points) == 0 && quality == "complete" {
			quality = "sampling"
		}
		result = append(result, Series{Labels: copyLabels(series.Labels), Points: points})
	}
	return result, quality
}

func deriveAverage(numerator, denominator []Series) ([]Series, string) {
	denominators := make(map[string]map[string]float64, len(denominator))
	for _, series := range denominator {
		values := make(map[string]float64, len(series.Points))
		for _, point := range series.Points {
			values[point.Timestamp] = point.Value
		}
		denominators[labelKey(series.Labels)] = values
	}
	result := make([]Series, 0, len(numerator))
	quality := "complete"
	for _, series := range numerator {
		values := denominators[labelKey(series.Labels)]
		points := make([]Point, 0, len(series.Points))
		for _, point := range series.Points {
			calls, ok := values[point.Timestamp]
			if !ok || calls <= 0 {
				quality = "zero_denominator"
				continue
			}
			points = append(points, Point{Timestamp: point.Timestamp, Value: point.Value / calls})
		}
		if len(points) == 0 && quality == "complete" {
			quality = "sampling"
		}
		result = append(result, Series{Labels: copyLabels(series.Labels), Points: points})
	}
	return result, quality
}

func secondsBetween(from, to string) float64 {
	start, err := time.Parse(time.RFC3339Nano, from)
	if err != nil {
		return 0
	}
	end, err := time.Parse(time.RFC3339Nano, to)
	if err != nil {
		return 0
	}
	return end.Sub(start).Seconds()
}

func copyLabels(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func dropLabels(values map[string]string, keys []string) map[string]string {
	result := copyLabels(values)
	for _, key := range keys {
		delete(result, key)
	}
	return result
}

func stripSeriesLabels(series []Series, keys []string) []Series {
	result := make([]Series, 0, len(series))
	for _, row := range series {
		result = append(result, Series{Labels: dropLabels(row.Labels, keys), Points: append([]Point(nil), row.Points...)})
	}
	return result
}
