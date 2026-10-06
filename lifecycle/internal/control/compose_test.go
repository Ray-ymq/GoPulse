package control

import (
	"context"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/lifecycle/internal/release"
)

func TestPrepareResolvesBackendAliasesAndPreservesRuntimeLabels(t *testing.T) {
	backend := release.Image{
		Ref:       "test/backend@sha256:" + strings.Repeat("a", 64),
		Platforms: map[string]string{"linux/amd64": "sha256:" + strings.Repeat("b", 64)},
	}
	frontend := release.Image{
		Ref:       "test/frontend@sha256:" + strings.Repeat("c", 64),
		Platforms: map[string]string{"linux/amd64": "sha256:" + strings.Repeat("d", 64)},
	}
	services := map[string]any{
		"edge":         map[string]any{"image": frontend.Ref, "ports": []any{"port"}},
		"backend":      map[string]any{"image": backend.Ref, "labels": map[string]any{"io.gopulse.runtime.process_id": "backend", "io.gopulse.runtime.role": "business-api"}},
		"backend-2":    map[string]any{"image": backend.Ref, "labels": map[string]any{"io.gopulse.runtime.process_id": "backend", "io.gopulse.runtime.role": "business-api"}},
		"platform-api": map[string]any{"image": backend.Ref, "labels": map[string]any{"io.gopulse.runtime.process_id": "backend", "io.gopulse.runtime.role": "platform-api"}},
	}
	c := &Controller{
		ctx:          context.Background(),
		manifest:     &release.Manifest{Images: map[string]release.Image{"backend": backend, "frontend": frontend}},
		manifestHash: "sha256:" + strings.Repeat("e", 64),
		state:        State{Token: "installation-token"},
		services:     services,
		doc:          map[string]any{"services": services},
	}
	if err := c.prepare(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"backend", "backend-2", "platform-api"} {
		service := services[name].(map[string]any)
		if service["image"] != "test/backend@sha256:"+strings.Repeat("b", 64) {
			t.Fatalf("%s image was not resolved to the platform digest: %v", name, service["image"])
		}
		labels := service["labels"].(map[string]any)
		if labels["io.gopulse.runtime.process_id"] != "backend" {
			t.Fatalf("%s runtime process label was lost: %#v", name, labels)
		}
		if labels["io.gopulse.lifecycle.resource"] != name {
			t.Fatalf("%s lifecycle label missing: %#v", name, labels)
		}
	}
	platformLabels := services["platform-api"].(map[string]any)["labels"].(map[string]any)
	if platformLabels["io.gopulse.runtime.role"] != "platform-api" {
		t.Fatalf("platform role label was overwritten: %#v", platformLabels)
	}
}
