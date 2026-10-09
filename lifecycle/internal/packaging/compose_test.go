package packaging

import (
	"encoding/json"
	"strings"
	"testing"
)

// composeServices mirrors the product Compose model: every runtime service
// plus the acceptance helper that never ships in a bundle.
func composeServices(extra map[string]any) map[string]any {
	names := []string{
		"backend", "backend-2", "platform-api", "migrate", "search-init", "admin-role",
		"business-worker", "business-worker-2", "search-indexer", "search-indexer-2",
		"router", "router-2", "marshaller", "marshaller-2", "monitor", "redis-exporter",
		"frontend", "admin-frontend", "redis", "mysql", "rabbitmq", "kafka", "kafka-init",
		"elasticsearch", "observability-elasticsearch", "victoriametrics",
	}
	services := map[string]any{}
	for _, name := range names {
		services[name] = map[string]any{"build": map[string]any{"context": ".."}, "ports": []any{"1:1"}}
	}
	services["acceptance"] = map[string]any{"image": "acceptance"}
	for name, service := range extra {
		services[name] = service
	}
	return services
}

func composeFixture(services map[string]any) []byte {
	document, _ := json.Marshal(map[string]any{
		"name":     "gopulse",
		"services": services,
		"volumes":  map[string]any{"data": map[string]any{"name": "gopulse_data"}},
		"networks": map[string]any{"internal": map[string]any{"name": "gopulse_internal"}},
		"secrets":  map[string]any{"tls": map[string]any{"name": "gopulse_tls"}},
	})
	return document
}

func renderedServices(t *testing.T, document []byte) map[string]map[string]any {
	t.Helper()
	rendered, err := productCompose(fixtureManifest("2.5.4"), document)
	if err != nil {
		t.Fatalf("product compose failed: %v", err)
	}
	var model struct {
		Services map[string]map[string]any `json:"services"`
		Volumes  map[string]map[string]any `json:"volumes"`
		Networks map[string]map[string]any `json:"networks"`
		Secrets  map[string]map[string]any `json:"secrets"`
	}
	if err := json.Unmarshal(rendered, &model); err != nil {
		t.Fatalf("rendered compose is not JSON: %v", err)
	}
	for kind, resources := range map[string]map[string]map[string]any{
		"volumes": model.Volumes, "networks": model.Networks, "secrets": model.Secrets,
	} {
		for name, resource := range resources {
			if _, ok := resource["name"]; ok {
				t.Errorf("%s %s keeps a project-scoped name", kind, name)
			}
		}
	}
	if _, ok := model.Services["acceptance"]; ok {
		t.Error("acceptance service must not ship in a product bundle")
	}
	return model.Services
}

// TestServiceMappingClosesCurrentCompose keeps the runtime service set closed:
// every service resolves to an immutable candidate reference and only the edge
// keeps published ports. Mirrors test_release_artifacts.test_service_mapping_closes_current_compose.
func TestServiceMappingClosesCurrentCompose(t *testing.T) {
	services := renderedServices(t, composeFixture(composeServices(nil)))
	edge, ok := services["edge"]
	if !ok {
		t.Fatal("edge service is missing")
	}
	if _, ok := edge["ports"]; !ok {
		t.Error("edge must own the published ports")
	}
	for name, service := range services {
		ref, _ := service["image"].(string)
		if !strings.HasPrefix(ref, "registry.example/gopulse/") {
			t.Errorf("%s image is not an immutable candidate reference: %q", name, ref)
		}
		if service["pull_policy"] != "always" {
			t.Errorf("%s must always pull by digest, got %v", name, service["pull_policy"])
		}
		if name == "edge" {
			continue
		}
		if _, ok := service["ports"]; ok {
			t.Errorf("%s must not publish ports", name)
		}
		if _, ok := service["build"]; ok {
			t.Errorf("%s must not keep a build section", name)
		}
	}
	if services["business-worker-2"]["image"] != services["business-worker"]["image"] {
		t.Error("business-worker-2 must reuse the business-worker image")
	}
	if services["observability-elasticsearch"]["image"] != services["elasticsearch"]["image"] {
		t.Error("observability-elasticsearch must reuse the elasticsearch image")
	}
	if services["backend-2"]["image"] != services["backend"]["image"] || services["platform-api"]["image"] != services["backend"]["image"] {
		t.Error("backend-2 and platform-api must reuse the backend image")
	}
}

// TestUnknownServiceIsRejected mirrors
// test_release_artifacts.test_unknown_service_is_rejected.
func TestUnknownServiceIsRejected(t *testing.T) {
	document := composeFixture(composeServices(map[string]any{"future-service": map[string]any{}}))
	_, err := productCompose(fixtureManifest("2.5.4"), document)
	if err == nil || !strings.Contains(err.Error(), "unmapped product service: future-service") {
		t.Fatalf("unmapped service must be rejected before packaging, got %v", err)
	}
}

// TestAliasTableMatchesRetainedDeliveryContract mirrors
// test_release_artifacts.test_aliases_cover_each_new_split_service and pins
// every entry of the table the retained Python delivery library still uses.
func TestAliasTableMatchesRetainedDeliveryContract(t *testing.T) {
	expected := map[string]string{
		"backend-2": "backend", "platform-api": "backend",
		"business-worker-2": "business-worker", "search-indexer-2": "search-indexer",
		"router-2": "router", "marshaller-2": "marshaller",
		"observability-elasticsearch": "elasticsearch",
		"migrate":                     "backend", "search-init": "backend", "admin-role": "backend",
		"kafka-init": "kafka",
	}
	if len(composeImageAliases) != len(expected) {
		t.Fatalf("alias table has %d entries, the delivery contract has %d", len(composeImageAliases), len(expected))
	}
	for service, logical := range expected {
		if composeImageAliases[service] != logical {
			t.Errorf("%s must map to %s, got %q", service, logical, composeImageAliases[service])
		}
	}
}
