package plugin

// CatalogEntry defines a server-owned identity, never a user-defined target.
// Ports and origins are private runtime policy and must not enter public DTOs.
type CatalogEntry struct {
	ID            string
	Source        string
	TargetID      string
	Entrypoint    string
	Port          int
	HostPort      int
	ContainerPort int
	Available     bool
}

// OfficialCatalog returns a fresh value so callers cannot mutate the trust policy.
// Availability denotes the exporter delivered in this batch, not installation.
func OfficialCatalog() []CatalogEntry {
	sources := [...]string{"redis", "mysql", "rabbitmq", "kafka", "elasticsearch", "victoriametrics"}
	ports := [...]int{6379, 3306, 15672, 9092, 9200, 8428}
	entries := make([]CatalogEntry, len(sources))
	for i, source := range sources {
		entries[i] = CatalogEntry{ID: source + "-exporter", Source: source, TargetID: source + "-exporter-local", Entrypoint: "bin/gopulse-" + source + "-exporter", Port: 9121 + i, HostPort: ports[i], ContainerPort: ports[i], Available: i == 0}
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
