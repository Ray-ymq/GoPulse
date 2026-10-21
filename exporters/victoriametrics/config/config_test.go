package config

import (
	"testing"
)

func TestControlledOriginAndAuthentication(t *testing.T) {
	for k, v := range map[string]string{"GOPULSE_RUNTIME_MODE": "container", "VICTORIAMETRICS_HOST": "victoriametrics", "VICTORIAMETRICS_PORT": "8428", "VICTORIAMETRICS_USERNAME": "u", "VICTORIAMETRICS_PASSWORD": "private-canary", "VICTORIAMETRICS_EXPORTER_CONNECT_TIMEOUT": "1s", "VICTORIAMETRICS_EXPORTER_SCRAPE_TIMEOUT": "2s", "VICTORIAMETRICS_EXPORTER_HTTP_HOST": "127.0.0.1", "VICTORIAMETRICS_EXPORTER_HTTP_PORT": "9126"} {
		t.Setenv(k, v)
	}
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VICTORIAMETRICS_USERNAME", "")
	if _, err := Load(); err == nil {
		t.Fatal("unauthenticated configuration accepted")
	}
}
