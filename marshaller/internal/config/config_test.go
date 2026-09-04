package config

import "testing"

func validEnv() map[string]string {
	return map[string]string{"MARSHALLER_API_TOKEN": "marshaller-token-at-least-32-bytes-long", "MARSHALLER_VM_PASSWORD": "development-vm-password"}
}
func loadMap(values map[string]string) (Config, error) {
	return LoadFrom(func(k string) (string, bool) { v, ok := values[k]; return v, ok })
}
func TestLoadDefaults(t *testing.T) {
	cfg, err := loadMap(validEnv())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KafkaTopic != Topic || cfg.KafkaGroup != Group || cfg.HTTPPort != 9093 || cfg.MaxRecordBytes != MaxRecordBytes || cfg.MaxOutputBytes != MaxOutputBytes {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}
func TestLoadRejectsUnsafeContracts(t *testing.T) {
	tests := map[string]map[string]string{
		"short token":        {"MARSHALLER_API_TOKEN": "short", "MARSHALLER_VM_PASSWORD": "development-vm-password"},
		"wrong topic":        {"MARSHALLER_API_TOKEN": "marshaller-token-at-least-32-bytes-long", "MARSHALLER_VM_PASSWORD": "development-vm-password", "MARSHALLER_KAFKA_TOPIC": "other"},
		"wrong group":        {"MARSHALLER_API_TOKEN": "marshaller-token-at-least-32-bytes-long", "MARSHALLER_VM_PASSWORD": "development-vm-password", "MARSHALLER_KAFKA_GROUP": "other"},
		"credentials in URL": {"MARSHALLER_API_TOKEN": "marshaller-token-at-least-32-bytes-long", "MARSHALLER_VM_PASSWORD": "development-vm-password", "MARSHALLER_VM_URL": "http://user:pass@127.0.0.1:8428"},
		"non loopback VM":    {"MARSHALLER_API_TOKEN": "marshaller-token-at-least-32-bytes-long", "MARSHALLER_VM_PASSWORD": "development-vm-password", "MARSHALLER_VM_URL": "http://192.0.2.1:8428"},
		"bad retry":          {"MARSHALLER_API_TOKEN": "marshaller-token-at-least-32-bytes-long", "MARSHALLER_VM_PASSWORD": "development-vm-password", "MARSHALLER_RETRY_MIN": "2s", "MARSHALLER_RETRY_MAX": "1s"},
	}
	for name, values := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := loadMap(values); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestLoadAcceptsIPv4AndIPv6LoopbackHTTPHosts(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "::1"} {
		t.Run(host, func(t *testing.T) {
			values := validEnv()
			values["MARSHALLER_HTTP_HOST"] = host
			cfg, err := loadMap(values)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.HTTPHost != host {
				t.Fatalf("host=%q, want %q", cfg.HTTPHost, host)
			}
		})
	}
}
