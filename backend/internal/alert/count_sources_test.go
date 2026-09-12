package alert

import (
	"context"
	"github.com/Ray-ymq/GoPulse/backend/internal/eventquery"
	"github.com/Ray-ymq/GoPulse/backend/internal/logquery"
	"testing"
	"time"
)

type countAdapter struct {
	panicValue bool
	value      int64
}

func (a countAdapter) AlertCount(context.Context, map[string]string, time.Time, time.Time) (int64, error) {
	if a.panicValue {
		panic("private upstream credentials")
	}
	return a.value, nil
}
func TestCountSources(t *testing.T) {
	for _, source := range []string{"logs", "events"} {
		t.Run(source, func(t *testing.T) {
			in := validInput()
			in.Source = source
			in.Reducer = "count"
			in.Selector = Selector{Labels: map[string]string{"service": "backend", "module": "auth", "message": "user registered"}}
			if source == "events" {
				in.Selector.Labels = map[string]string{"source": "monitor", "event_name": "exporter_plugin_started", "plugin_id": "redis-exporter", "operation": "start"}
			}
			if Validate(in, true) != nil {
				t.Fatal("valid count rejected")
			}
			for k, v := range map[string]string{"request_id": "123", "query": "*", "index": "x", "source": "*"} {
				bad := in
				bad.Selector = Selector{Labels: map[string]string{k: v}}
				if Validate(bad, true) == nil {
					t.Fatal("unsafe selector accepted", k)
				}
			}
			bad := in
			bad.Reducer = "last"
			if Validate(bad, true) == nil {
				t.Fatal("non-count accepted")
			}
			in.Revision = 1
			r := Rule{Input: in}
			s := NewScheduler(nil, nil).WithCounts(countAdapter{}, countAdapter{})
			if v, ok := s.value(context.Background(), r, time.Now()); !ok || v != 0 {
				t.Fatal("real zero lost")
			}
			s.WithCounts(countAdapter{panicValue: true}, countAdapter{panicValue: true})
			if _, ok := s.value(context.Background(), r, time.Now()); ok {
				t.Fatal("panic became known")
			}
			s.WithCounts(countAdapter{value: -1}, countAdapter{value: -1})
			if _, ok := s.value(context.Background(), r, time.Now()); ok {
				t.Fatal("negative count became known")
			}
		})
	}
}
func TestCountCatalogCombinations(t *testing.T) {
	for _, tuple := range logquery.AlertCatalog().Tuples {
		if !logquery.ValidateAlertSelector(tuple) {
			t.Fatal(tuple)
		}
	}
	for _, tuple := range eventquery.AlertCatalog().Tuples {
		if !eventquery.ValidateAlertSelector(tuple) {
			t.Fatal(tuple)
		}
	}
	if logquery.ValidateAlertSelector(map[string]string{"service": "backend", "module": "auth", "message": "post created"}) || eventquery.ValidateAlertSelector(map[string]string{"event_name": "exporter_plugin_started", "error_code": "start_failed"}) {
		t.Fatal("invalid combination")
	}
}
