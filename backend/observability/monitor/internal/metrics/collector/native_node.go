package collector

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/envelope"
	"github.com/Ray-ymq/GoPulse/runtimekit/metriccontract"
)

// parseNativeNode 保留官方指标名称，由 Monitor 生成本轮采集状态。
func parseNativeNode(status int, body []byte) (string, []envelope.Sample, error) {
	if status != http.StatusOK {
		return "", nil, errors.New("http_invalid")
	}
	samples, err := parseExposition("node", body)
	if err != nil {
		return "", nil, err
	}
	required := map[string]bool{"node_cpu_seconds_total": false, "node_memory_MemTotal_bytes": false, "node_load1": false}
	success, collectors := true, 0
	for _, sample := range samples {
		if !strings.HasPrefix(sample.Name, "node_") {
			return "", nil, errors.New("contract_invalid")
		}
		if _, ok := required[sample.Name]; ok {
			required[sample.Name] = true
		}
		if sample.Name == "node_scrape_collector_success" {
			collectors++
			if sample.Kind != "gauge" || len(sample.Labels) != 1 || sample.Labels["collector"] == "" || (sample.Value != 0 && sample.Value != 1) {
				return "", nil, errors.New("contract_invalid")
			}
			success = success && sample.Value == 1
		}
	}
	if collectors == 0 {
		return "", nil, errors.New("contract_invalid")
	}
	for _, found := range required {
		if !found && success {
			return "", nil, errors.New("contract_invalid")
		}
	}
	up := envelope.Sample{Name: "gopulse_node_up", Kind: "gauge", Labels: map[string]string{}, Value: 1}
	if !success {
		up.Value = 0
		return "target_unavailable", []envelope.Sample{up}, nil
	}
	if len(samples) >= metriccontract.MaxSamples {
		return "", nil, errors.New("contract_invalid")
	}
	samples = append(samples, up)
	checked := make([]metriccontract.Sample, len(samples))
	for i, sample := range samples {
		checked[i] = metriccontract.Sample{Name: sample.Name, Kind: sample.Kind, Labels: sample.Labels, Value: sample.Value}
	}
	if metriccontract.Validate("node", checked) != nil {
		return "", nil, errors.New("contract_invalid")
	}
	return "success", samples, nil
}
