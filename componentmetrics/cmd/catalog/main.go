// Command catalog emits the frozen component contract for documentation and the
// browser's strict DTO validator. It contains no runtime configuration/secrets.
package main

import (
	"encoding/json"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"os"
)

func main() {
	var specs []componentmetrics.Spec
	for _, id := range componentmetrics.Components {
		s, _ := componentmetrics.Catalog(id)
		specs = append(specs, s)
	}
	if err := json.NewEncoder(os.Stdout).Encode(specs); err != nil {
		os.Exit(1)
	}
}
