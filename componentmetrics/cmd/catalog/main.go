// Command catalog emits the frozen component/metric directory for documentation
// and the browser's strict DTO validator. It contains no runtime configuration
// or secrets. --processes emits the closed process IDs used by contract checks.
package main

import (
	"encoding/json"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"os"
)

func main() {
	if len(os.Args) > 1 {
		if len(os.Args) != 2 || os.Args[1] != "--processes" {
			os.Exit(2)
		}
		if err := json.NewEncoder(os.Stdout).Encode(componentmetrics.ProcessIDs()); err != nil {
			os.Exit(1)
		}
		return
	}
	var specs []componentmetrics.Spec
	for _, id := range componentmetrics.Components {
		s, _ := componentmetrics.Catalog(id)
		specs = append(specs, s)
	}
	if err := json.NewEncoder(os.Stdout).Encode(specs); err != nil {
		os.Exit(1)
	}
}
