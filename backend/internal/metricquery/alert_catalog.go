package metricquery

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
	out := make([]AlertDefinition, 0, len(Presets))
	for _, d := range Presets {
		a := AlertDefinition{Metric: d.Metric, Kind: d.Kind, Unit: d.Unit, Keys: []string{}, Tuples: [][]string{{}}, Reducers: []string{"last", "max", "min", "avg"}}
		if d.Kind == "counter" {
			a.Reducers = []string{"increase"}
		}
		if len(d.LabelKeys) > 0 && d.label == "" {
			// Dynamic exporter dimensions (queue, node, command, operation, ...)
			// do not have a code-owned finite tuple set and therefore are not
			// exposed as exact alert selectors.
			continue
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
			default:
				continue
			}
			for _, v := range values {
				a.Tuples = append(a.Tuples, []string{v})
			}
		}
		out = append(out, a)
	}
	return out
}
