package componentmetrics

import (
	"errors"
	"math"
	"strings"
	"unicode/utf8"
)

type Sample struct {
	Name   string            `json:"name"`
	Kind   string            `json:"kind"`
	Labels map[string]string `json:"labels"`
	Value  float64           `json:"value"`
}

func validValue(f Family, v float64) bool {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return false
	}
	if f.Kind != "gauge" && f.Kind != "counter" {
		return false
	}
	if strings.HasSuffix(f.Name, "dependency_up") {
		return v == -1 || v == 0 || v == 1
	}
	if v < 0 {
		return false
	}
	if f.Unit == "count" || f.Unit == "bytes" || f.Unit == "unix_seconds" {
		return v == math.Trunc(v)
	}
	return true
}
func ValidateSample(id string, s Sample) error {
	spec, ok := Catalog(id)
	if !ok {
		return errors.New("invalid component")
	}
	if len(s.Name) > 128 {
		return errors.New("invalid component sample")
	}
	for key, value := range s.Labels {
		if len(key) > 64 || len(value) > 128 || !utf8.ValidString(key) || !utf8.ValidString(value) {
			return errors.New("invalid component labels")
		}
	}
	for _, f := range spec.Families {
		if f.Name == s.Name {
			if s.Kind != f.Kind || len(s.Labels) != len(f.Keys) || !validValue(f, s.Value) {
				break
			}
			values := make([]string, len(f.Keys))
			for i, k := range f.Keys {
				v, ok := s.Labels[k]
				if !ok {
					return errors.New("invalid component labels")
				}
				values[i] = v
			}
			for _, allowed := range f.Tuples {
				if tupleKey(allowed) == tupleKey(values) {
					return nil
				}
			}
			break
		}
	}
	return errors.New("invalid component sample")
}

// Validate is called independently at both ingress boundaries. It requires all
// fixed gauge tuples, admits genuinely unobserved counter tuples, and insists on
// the matching count/duration tuple whenever either one is present.
func Validate(id string, samples []Sample) error {
	spec, ok := Catalog(id)
	if !ok || len(samples) > spec.MaxSamples {
		return errors.New("invalid component sample budget")
	}
	seen := make(map[string]bool, len(samples))
	byKey := make(map[string]Sample, len(samples))
	key := func(name string, labels map[string]string) string {
		var b strings.Builder
		b.WriteString(name)
		for _, k := range sortedKeys(labels) {
			b.WriteByte(0)
			b.WriteString(k)
			b.WriteByte(0)
			b.WriteString(labels[k])
		}
		return b.String()
	}
	for _, s := range samples {
		if err := ValidateSample(id, s); err != nil {
			return err
		}
		k := key(s.Name, s.Labels)
		if seen[k] {
			return errors.New("duplicate component series")
		}
		seen[k] = true
		byKey[k] = s
	}
	for _, f := range spec.Families {
		for _, tuple := range f.Tuples {
			labels := Labels(f, tuple)
			present := seen[key(f.Name, labels)]
			if f.Required && !present {
				return errors.New("missing component gauge")
			}
			if f.Pair != "" && present != seen[key(f.Pair, labels)] {
				return errors.New("unpaired component counter")
			}
		}
	}
	if err := validateDistributions(spec, seen, byKey, key); err != nil {
		return err
	}
	return nil
}

func validateDistributions(spec Spec, seen map[string]bool, samples map[string]Sample, key func(string, map[string]string) string) error {
	groups := make(map[string][]Family)
	for _, family := range spec.Families {
		if family.Distribution == nil {
			continue
		}
		groups[family.Distribution.Name] = append(groups[family.Distribution.Name], family)
	}
	for _, families := range groups {
		var bucket, count, sum *Family
		for i := range families {
			switch families[i].Distribution.Role {
			case "bucket":
				bucket = &families[i]
			case "count":
				count = &families[i]
			case "sum":
				sum = &families[i]
			default:
				return errors.New("invalid component distribution role")
			}
		}
		if bucket == nil || count == nil || sum == nil || len(bucket.Distribution.Buckets) == 0 {
			return errors.New("invalid component distribution definition")
		}
		for _, tuple := range count.Tuples {
			countLabels := Labels(*count, tuple)
			sumLabels := Labels(*sum, tuple)
			countKey := key(count.Name, countLabels)
			sumKey := key(sum.Name, sumLabels)
			present := seen[countKey] || seen[sumKey]
			bucketSamples := make([]Sample, len(bucket.Distribution.Buckets))
			for i, le := range bucket.Distribution.Buckets {
				bucketLabels := Labels(*bucket, append(append([]string(nil), tuple...), le))
				bucketKey := key(bucket.Name, bucketLabels)
				if seen[bucketKey] {
					present = true
				}
				if sample, ok := samples[bucketKey]; ok {
					bucketSamples[i] = sample
				}
			}
			if !present {
				continue
			}
			if !seen[countKey] || !seen[sumKey] {
				return errors.New("incomplete component distribution")
			}
			previous := -1.0
			for _, sample := range bucketSamples {
				if sample.Name == "" || sample.Value < previous {
					return errors.New("non-cumulative component buckets")
				}
				previous = sample.Value
			}
			if bucketSamples[len(bucketSamples)-1].Value != samples[countKey].Value {
				return errors.New("component bucket count mismatch")
			}
		}
	}
	return nil
}
