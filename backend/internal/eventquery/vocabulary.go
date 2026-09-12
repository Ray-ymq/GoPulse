package eventquery

import (
	"github.com/Ray-ymq/GoPulse/backend/internal/alert/count"
	"sort"
)

func validFilters(filters Filters) bool {
	for k, v := range map[string]string{"source": filters.Source, "plugin_id": filters.PluginID, "severity": filters.Severity, "operation": filters.Operation, "error_code": filters.ErrorCode} {
		if !eventMember(k, v) {
			return false
		}
	}
	if filters.EventName != "" {
		severity, ok := eventSeverities[filters.EventName]
		if !ok || (filters.Severity != "" && filters.Severity != severity) {
			return false
		}
	}
	if filters.EventName != "" && filters.Operation != "" && !eventOperation(filters.EventName, filters.Operation) {
		return false
	}
	if filters.ErrorCode != "" {
		if !knownErrorCode(filters.ErrorCode) || (filters.EventName != "" && filters.EventName != "exporter_plugin_failed" && filters.EventName != "exporter_plugin_exited" && filters.EventName != "metrics_collection_failed") {
			return false
		}
		if filters.Operation != "" && !operationError(filters.Operation, filters.ErrorCode) {
			return false
		}
		if filters.EventName != "" && !eventError(filters.EventName, filters.ErrorCode) {
			return false
		}
	}
	return true
}

func eventOperation(name, operation string) bool {
	allowed := map[string]map[string]bool{
		"exporter_plugin_installed": {"install": true}, "exporter_plugin_started": {"start": true}, "exporter_plugin_stopped": {"stop": true}, "exporter_plugin_updated": {"update": true},
		"exporter_plugin_failed": {"start": true, "stop": true, "update": true, "recover": true}, "exporter_plugin_exited": {"start": true},
		"metrics_collection_failed": {"scrape": true, "publish": true}, "metrics_collection_recovered": {"scrape": true}, "metrics_target_unavailable": {"scrape": true}, "metrics_target_recovered": {"scrape": true},
	}
	return allowed[name][operation]
}

func knownErrorCode(code string) bool { return code != "" && eventMember("error_code", code) }

func eventError(name, code string) bool {
	allowed := map[string]map[string]bool{
		"exporter_plugin_failed": {
			"start_failed": true, "stop_failed": true, "update_failed": true, "rollback_failed": true, "recovery_failed": true, "recovery_invalid": true,
		},
		"exporter_plugin_exited": {"process_exited": true},
		"metrics_collection_failed": {
			"scrape_timeout": true, "network_failed": true, "response_too_large": true, "parse_failed": true, "contract_invalid": true, "content_invalid": true, "http_invalid": true, "scrape_failed": true, "message_id_failed": true, "publish_failed": true,
		},
	}
	return allowed[name][code]
}

func operationError(operation, code string) bool {
	allowed := map[string]map[string]bool{
		"start": {"start_failed": true, "process_exited": true}, "stop": {"stop_failed": true}, "update": {"update_failed": true, "rollback_failed": true}, "recover": {"recovery_failed": true, "recovery_invalid": true},
		"scrape":  {"scrape_timeout": true, "network_failed": true, "response_too_large": true, "parse_failed": true, "contract_invalid": true, "content_invalid": true, "http_invalid": true, "scrape_failed": true, "message_id_failed": true},
		"publish": {"publish_failed": true},
	}
	return allowed[operation][code]
}

func AlertCatalog() count.Catalog {
	c := count.Catalog{Fields: map[string][]string{}, Reducers: []string{"count"}, MinimumSelectors: 1}
	for k, v := range eventFields {
		c.Fields[k] = append([]string(nil), v...)
	}
	for name := range eventSeverities {
		c.Fields["event_name"] = append(c.Fields["event_name"], name)
	}
	sort.Strings(c.Fields["event_name"])
	for _, name := range c.Fields["event_name"] {
		for _, severity := range c.Fields["severity"] {
			for _, op := range c.Fields["operation"] {
				for _, code := range append([]string{""}, c.Fields["error_code"]...) {
					if validFilters(Filters{EventName: name, Severity: severity, Operation: op, ErrorCode: code}) {
						tuple := map[string]string{"event_name": name, "severity": severity, "operation": op}
						if code != "" {
							tuple["error_code"] = code
						}
						c.Tuples = append(c.Tuples, tuple)
					}
				}
			}
		}
	}
	return c
}
func ValidateAlertSelector(labels map[string]string) bool {
	if len(labels) == 0 {
		return false
	}
	for k, v := range labels {
		if v == "" {
			return false
		}
		if k == "event_name" {
			if _, ok := eventSeverities[v]; !ok {
				return false
			}
			continue
		}
		if _, ok := eventFields[k]; !ok || !eventMember(k, v) {
			return false
		}
	}
	return validFilters(Filters{Source: labels["source"], EventName: labels["event_name"], Severity: labels["severity"], PluginID: labels["plugin_id"], Operation: labels["operation"], ErrorCode: labels["error_code"]})
}

var eventFields = map[string][]string{"source": {"monitor"}, "severity": {"info", "warn", "error"}, "plugin_id": {"redis-exporter", "mysql-exporter", "rabbitmq-exporter", "kafka-exporter", "elasticsearch-exporter", "victoriametrics-exporter"}, "operation": {"install", "start", "stop", "update", "recover", "scrape", "publish"}, "error_code": {"start_failed", "stop_failed", "update_failed", "rollback_failed", "recovery_failed", "recovery_invalid", "process_exited", "scrape_timeout", "network_failed", "response_too_large", "parse_failed", "contract_invalid", "content_invalid", "http_invalid", "scrape_failed", "message_id_failed", "publish_failed"}}

func eventMember(key, value string) bool {
	if value == "" {
		return true
	}
	for _, v := range eventFields[key] {
		if v == value {
			return true
		}
	}
	return false
}

var eventSeverities = map[string]string{
	"exporter_plugin_installed": "info", "exporter_plugin_started": "info", "exporter_plugin_stopped": "info", "exporter_plugin_updated": "info",
	"exporter_plugin_failed": "error", "exporter_plugin_exited": "error", "metrics_collection_failed": "warn", "metrics_collection_recovered": "info",
	"metrics_target_unavailable": "warn", "metrics_target_recovered": "info",
}
