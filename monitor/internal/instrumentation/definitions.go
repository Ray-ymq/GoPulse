// Metric definitions belong to this producer module. Label tuples are bounded.
package instrumentation

import (
	_ "embed"
	"encoding/json"
	"strings"
)

type Family struct {
	Name, Kind, Unit string
	Keys             []string
	Tuples           [][]string
	Required         bool
	Pair             string
	Distribution     *Distribution
}
type Distribution struct {
	Name, Role string
	Buckets    []string
}
type Spec struct {
	ID                       string
	Families                 []Family
	MaxSamples, MaxBodyBytes int
}

//go:embed definitions.json
var definitions []byte
var specs = func() map[string]Spec {
	var all []Spec
	if err := json.Unmarshal(definitions, &all); err != nil {
		panic(err)
	}
	out := map[string]Spec{}
	for _, s := range all {
		out[s.ID] = s
	}
	return out
}()

func Catalog(id string) (Spec, bool)  { s, ok := specs[id]; return s, ok }
func Prefix(id string) string         { return "gopulse_" + strings.ReplaceAll(id, "-", "_") + "_" }
func tupleKey(values []string) string { return strings.Join(values, "\x00") }
