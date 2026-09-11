package componentmetrics

import (
	"errors"
	"math"
	"strings"
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
	return nil
}
