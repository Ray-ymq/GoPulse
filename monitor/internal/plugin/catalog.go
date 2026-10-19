package plugin

// CatalogEntry defines a server-owned identity, never a user-defined target.
// Ports and origins are private runtime policy and must not enter public DTOs.
type CatalogEntry struct {
	SchemaVersion          int          `json:"schema_version"`
	ID                     string       `json:"id"`
	Name                   string       `json:"name"`
	Source                 string       `json:"source"`
	TargetID               string       `json:"target_id"`
	Entrypoint             string       `json:"entrypoint"`
	HealthPath             string       `json:"health_path"`
	MetricsPath            string       `json:"metrics_path"`
	Port                   int          `json:"listen_port"`
	HostPort               int          `json:"host_port"`
	ContainerPort          int          `json:"container_port"`
	RuntimeContractVersion int          `json:"runtime_contract_version"`
	MetricsContractVersion int          `json:"metrics_contract_version"`
	ConfigSchemaPath       string       `json:"config_schema_path"`
	Schema                 ConfigSchema `json:"schema,omitempty"`
	Available              bool         `json:"available"`
}

// OfficialCatalog returns a fresh value so callers cannot mutate the trust policy.
// Availability denotes the exporter delivered in this batch, not installation.
func OfficialCatalog() []CatalogEntry {
	sources := [...]string{"redis", "mysql", "rabbitmq", "kafka", "elasticsearch", "victoriametrics"}
	ports := [...]int{6379, 3306, 15672, 9092, 9200, 8428}
	entries := make([]CatalogEntry, len(sources))
	for i, source := range sources {
		entries[i] = CatalogEntry{ID: source + "-exporter", Name: "GoPulse " + source + " Exporter", Source: source, TargetID: source + "-exporter-local", Entrypoint: "bin/gopulse-" + source + "-exporter", HealthPath: "/health", MetricsPath: "/metrics", Port: 9121 + i, HostPort: ports[i], ContainerPort: ports[i], RuntimeContractVersion: 1, MetricsContractVersion: 2, ConfigSchemaPath: "config.schema.json", Available: true}
		if source == "kafka" {
			entries[i].ContainerPort = 19092
		}
	}
	return entries
}

func LookupOfficial(id string) (CatalogEntry, bool) {
	for _, entry := range OfficialCatalog() {
		if entry.ID == id {
			return entry, true
		}
	}
	return CatalogEntry{}, false
}
