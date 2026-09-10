package plugin

import "encoding/json"

type PublicCatalogEntry struct {
	ID               string       `json:"id"`
	Name             string       `json:"name"`
	Source           string       `json:"source"`
	Available        bool         `json:"available"`
	Schema           ConfigSchema `json:"schema"`
	Configured       bool         `json:"configured"`
	SecretConfigured bool         `json:"secret_configured"`
	Revision         string       `json:"revision"`
	Summary          string       `json:"summary"`
}

func (m *Manager) Catalog() []PublicCatalogEntry {
	out := []PublicCatalogEntry{}
	if m.core != nil {
		m.core.mu.RLock()
		defer m.core.mu.RUnlock()
	}
	for _, entry := range OfficialCatalog() {
		schema, _ := OfficialSchema(entry.ID)
		item := PublicCatalogEntry{ID: entry.ID, Name: "GoPulse " + entry.Source + " Exporter", Source: entry.Source, Schema: schema, Summary: "not_configured"}
		if m.core != nil {
			_, err := m.core.current(entry.ID)
			item.Available = err == nil
			if active := m.core.slots[entry.ID].active; active != nil {
				item.Configured = true
				var secrets map[string]string
				if json.Unmarshal(active.Secret, &secrets) == nil {
					for _, value := range secrets {
						item.SecretConfigured = item.SecretConfigured || value != ""
					}
				}
				item.Revision = active.ID
				item.Summary = "configured"
				if active.Entry.Manifest.SchemaVersion == 1 {
					item.Summary = "upgrade_required"
				}
			}
		}
		out = append(out, item)
	}
	return out
}
