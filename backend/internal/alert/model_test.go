package alert

import (
	"encoding/json"
	"github.com/Ray-ymq/GoPulse/backend/internal/metricquery"
	"testing"
	"time"
)

func validInput() Input {
	b := true
	v := 1.0
	return Input{Name: "Redis", Enabled: &b, Severity: "warning", Source: "metrics", Selector: Selector{Metric: "gopulse_redis_up", Labels: map[string]string{}}, Reducer: "last", Operator: "lt", Threshold: &v, Window: "5m", For: "0s"}
}
func TestExactCatalogValidation(t *testing.T) {
	for _, d := range metricquery.AlertCatalog() {
		if len(d.Tuples) == 0 {
			t.Fatalf("missing tuples: %s", d.Metric)
		}
		in := validInput()
		in.Selector.Metric = d.Metric
		in.Selector.Labels = map[string]string{}
		for i, k := range d.Keys {
			in.Selector.Labels[k] = d.Tuples[0][i]
		}
		in.Reducer = d.Reducers[0]
		if e := Validate(in, true); e != nil {
			t.Fatalf("%s: %v", d.Metric, e)
		}
		in.Selector.Labels["unexpected"] = "x"
		if Validate(in, true) == nil {
			t.Fatalf("extra label accepted: %s", d.Metric)
		}
	}
	for _, mutate := range []func(*Input){func(i *Input) { i.Source = "logs" }, func(i *Input) { i.Selector.Metric = "unknown" }, func(i *Input) { i.Reducer = "increase" }, func(i *Input) { i.Threshold = nil }, func(i *Input) { i.Enabled = nil }, func(i *Input) { i.Window = "1m"; i.For = "5m" }, func(i *Input) { i.Name = "\n" }} {
		in := validInput()
		mutate(&in)
		if Validate(in, true) == nil {
			t.Fatal("invalid rule accepted")
		}
	}
}
func TestReducersAndUnknown(t *testing.T) {
	cutoff := time.Date(2026, 9, 12, 0, 5, 0, 0, time.UTC)
	points := func(values ...float64) []metricquery.Point {
		out := []metricquery.Point{}
		for i, v := range values {
			out = append(out, metricquery.Point{Timestamp: cutoff.Add(time.Duration(i-len(values)+1) * 15 * time.Second).Format(time.RFC3339Nano), Value: v})
		}
		return out
	}
	for _, tt := range []struct {
		r      string
		values []float64
		want   float64
	}{{"last", []float64{2, 4, 3}, 3}, {"min", []float64{2, 4, 3}, 2}, {"max", []float64{2, 4, 3}, 4}, {"avg", []float64{2, 4, 3}, 3}, {"increase", []float64{8, 12, 2, 5}, 9}} {
		v, ok := Reduce(points(tt.values...), tt.r, cutoff)
		if !ok || v != tt.want {
			t.Fatalf("%s: %v %v", tt.r, v, ok)
		}
	}
	if _, ok := Reduce(nil, "last", cutoff); ok {
		t.Fatal("empty became zero")
	}
	if _, ok := Reduce(points(1), "increase", cutoff); ok {
		t.Fatal("one counter point accepted")
	}
	if _, ok := Reduce(points(1), "last", cutoff.Add(91*time.Second)); ok {
		t.Fatal("stale accepted")
	}
	if v, ok := Reduce(points(0), "last", cutoff); !ok || v != 0 {
		t.Fatal("real zero lost")
	}
}
func TestStrictJSON(t *testing.T) {
	raw, _ := json.Marshal(validInput())
	var in Input
	if e := strict(raw, &in); e != nil {
		t.Fatal(e)
	}
	for _, raw := range [][]byte{[]byte(`{"name":"a","name":"b"}`), []byte(`{"selector":{"metric":"x","metric":"y"}}`), []byte(`{} {}`), []byte(`{"unknown":1}`), []byte(`{"threshold":1e999}`), []byte("{\"name\":\"\xff\"}"), []byte(`null`)} {
		if strict(raw, &Input{}) == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}
