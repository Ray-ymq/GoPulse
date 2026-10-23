package runtimekit

import "strings"

var Components = []string{"backend", "business-worker", "search-indexer", "monitor", "marshaller"}
var Plugins = []string{"redis", "mysql", "rabbitmq", "kafka", "elasticsearch", "victoriametrics"}

// ProcessIDs is the code-owned process identity inventory. Runtime roles,
// replica ownership, budgets and Compose bindings remain authoritative in
// deploy/runtime-contracts.json; the verifier rejects drift between this
// process inventory and that machine contract.
func ProcessIDs() []string {
	result := append([]string{}, Components...)
	for _, plugin := range Plugins {
		result = append(result, plugin+"-exporter")
	}
	return result
}

func IsComponent(id string) bool {
	for _, c := range Components {
		if c == id {
			return true
		}
	}
	return false
}
func Prefix(id string) string { return "gopulse_" + strings.ReplaceAll(id, "-", "_") + "_" }
func Target(id string) string { return id + "-local" }
