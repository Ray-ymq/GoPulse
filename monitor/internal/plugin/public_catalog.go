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
	entries := OfficialCatalog()
	if m.core != nil {
		entries = catalogEntries(m.core.catalog)
	}
	for _, entry := range entries {
		schema := entry.Schema
		if len(schema.Fields) == 0 {
			schema, _ = OfficialSchema(entry.ID)
		}
		name := entry.Name
		if name == "" {
			name = "GoPulse " + entry.Source + " Exporter"
		}
		item := PublicCatalogEntry{ID: entry.ID, Name: name, Source: entry.Source, Available: entry.Available, Schema: schema, Summary: "not_configured"}
		if m.core != nil {
			_, err := m.core.current(entry.ID)
			item.Available = item.Available && err == nil
			if slot := m.core.slots[entry.ID]; slot != nil && slot.active != nil {
				active := slot.active
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

// ExecutionCatalog exposes only the deployment descriptors needed to create
// generic scrape tasks. It deliberately excludes package paths and secrets.
func (m *Manager) ExecutionCatalog() []CatalogEntry {
	if m == nil || m.core == nil {
		return nil
	}
	return catalogEntries(m.core.catalog)
}
