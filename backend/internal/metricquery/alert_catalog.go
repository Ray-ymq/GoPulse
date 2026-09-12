package metricquery

import "github.com/Ray-ymq/GoPulse/componentmetrics"

// AlertDefinition exposes only the fixed, exact non-identity selector vocabulary.
type AlertDefinition struct {
	Metric   string     `json:"metric"`
	Kind     string     `json:"kind"`
	Unit     string     `json:"unit"`
	Keys     []string   `json:"label_keys"`
	Tuples   [][]string `json:"allowed_tuples"`
	Reducers []string   `json:"reducers"`
}

func AlertCatalog() []AlertDefinition {
	out := make([]AlertDefinition, 0, len(Catalog))
	for _, d := range Catalog {
		a := AlertDefinition{Metric: d.Metric, Kind: d.Kind, Unit: d.Unit, Keys: []string{}, Tuples: [][]string{{}}, Reducers: []string{"last", "max", "min", "avg"}}
		if d.Kind == "counter" {
			a.Reducers = []string{"increase"}
		}
		if componentmetrics.IsComponent(d.Source) {
			spec, _ := componentmetrics.Catalog(d.Source)
			for _, f := range spec.Families {
				if f.Name == d.Metric {
					if len(f.Keys) > 0 {
						a.Keys = f.Keys
						a.Tuples = f.Tuples
					}
					break
				}
			}
		} else if d.label != "" {
			a.Keys = []string{d.label}
			a.Tuples = nil
			var values []string
			switch d.label {
			case "mode":
				values = []string{"user", "system"}
			case "result":
				values = []string{"commit", "rollback"}
			case "state":
				values = []string{"ready", "unacked"}
			case "status":
				values = []string{"green", "yellow", "red"}
			case "db":
				values = []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"}
			}
			for _, v := range values {
				a.Tuples = append(a.Tuples, []string{v})
			}
		}
		out = append(out, a)
	}
	return out
}
