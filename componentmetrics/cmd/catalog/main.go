// Command catalog emits the frozen component/metric directory for documentation
// and the browser's strict DTO validator. It contains no runtime configuration
// or secrets. --processes emits the closed process IDs used by contract checks.
package main

import (
	"encoding/json"
	"fmt"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"os"
	"sort"
	"strings"
)

func main() {
	if len(os.Args) > 1 {
		if len(os.Args) != 2 || (os.Args[1] != "--processes" && os.Args[1] != "--typescript") {
			os.Exit(2)
		}
		if os.Args[1] == "--typescript" {
			if err := emitTypeScript(); err != nil {
				os.Exit(1)
			}
			return
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

func emitTypeScript() error {
	var specs []componentmetrics.Spec
	for _, id := range componentmetrics.Components {
		s, _ := componentmetrics.Catalog(id)
		specs = append(specs, s)
	}
	names := make([]string, 0)
	contracts := make(map[string]any)
	for _, spec := range specs {
		for _, family := range spec.Families {
			keys := append([]string{}, family.Keys...)
			tuples := append([][]string(nil), family.Tuples...)
			contract := map[string]any{"source": spec.ID, "kind": family.Kind, "unit": family.Unit, "keys": keys, "tuples": tuples}
			if family.Distribution != nil {
				contract["distribution"] = map[string]any{"name": family.Distribution.Name, "role": family.Distribution.Role, "buckets": family.Distribution.Buckets}
			}
			contracts[family.Name] = contract
			names = append(names, family.Name)
		}
	}
	sort.Strings(names)
	body, err := json.Marshal(contracts)
	if err != nil {
		return err
	}
	var out strings.Builder
	out.WriteString("// Generated from componentmetrics Catalog; no runtime configuration or credentials.\n")
	out.WriteString("export type ComponentMetricName = '")
	out.WriteString(strings.Join(names, "' | '") + "'\n")
	out.WriteString("export interface ComponentContract { source: string; kind: 'counter'|'gauge'; unit: 'count'|'seconds'|'bytes'|'state'|'unix_seconds'; keys: string[]; tuples: string[][]; distribution?: { name: string; role: 'bucket'|'count'|'sum'; buckets: string[] } }\n")
	fmt.Fprintf(&out, "export const componentContracts: Record<ComponentMetricName, ComponentContract> = %s\n", body)
	out.WriteString("export function componentContract(name: string): ComponentContract | undefined { return Object.hasOwn(componentContracts, name) ? componentContracts[name as ComponentMetricName] : undefined }\n")
	out.WriteString("export function validComponentLabels(contract: ComponentContract, labels: Record<string, unknown>): boolean {\n return Object.keys(labels).length === contract.keys.length && contract.tuples.some(tuple => contract.keys.every((key, i) => labels[key] === tuple[i]))\n}\n")
	out.WriteString("export function validComponentValue(name: string, contract: ComponentContract, value: number): boolean {\n if (!Number.isFinite(value)) return false\n if (name.endsWith('_dependency_up')) return value === -1 || value === 0 || value === 1\n return value >= 0 && (contract.unit === 'seconds' || Number.isInteger(value))\n}\n")
	_, err = os.Stdout.WriteString(out.String())
	return err
}
