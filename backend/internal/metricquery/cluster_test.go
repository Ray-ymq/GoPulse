package metricquery

import (
	"strings"
	"testing"
)

func TestClusterProvenanceAndEnumLabels(t *testing.T) {
	for _, name := range []string{"gopulse_mysql_transactions_total", "gopulse_rabbitmq_messages", "gopulse_kafka_up", "gopulse_elasticsearch_cluster_health_status", "gopulse_victoriametrics_storage_rows_deleted_total"} {
		definition := definitions[name]
		labels := map[string]string{"__name__": name, "source": definition.Source, "target_id": definition.TargetID, "producer_kind": "exporter_plugin", "producer_id": definition.ProducerID}
		if definition.Source == "mysql" {
			labels["result"] = "commit"
		} else if definition.Source == "elasticsearch" {
			labels["status"] = "yellow"
		} else if definition.Source == "rabbitmq" {
			labels["state"] = "ready"
		}
		if _, _, err := validateLabels(labels, definition); err != nil {
			t.Fatal(err)
		}
		if query := QueryExpression(name); !strings.Contains(query, `producer_id="`+definition.ProducerID+`"`) {
			t.Fatal("unscoped query")
		}
		labels["producer_id"] = "redis-exporter"
		if _, _, err := validateLabels(labels, definition); err == nil {
			t.Fatal("foreign producer accepted")
		}
	}
}
