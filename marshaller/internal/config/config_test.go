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
	if cfg.InstanceID != "marshaller-local" || cfg.ReplicaCount != 1 || cfg.KafkaTopic != Topic || cfg.KafkaGroup != Group || cfg.KafkaMinPartitions != 1 || cfg.MaxInFlight != 4 || cfg.MaxRetrying != 4 || cfg.HTTPPort != 9093 || cfg.MaxRecordBytes != MaxRecordBytes || cfg.MaxOutputBytes != MaxOutputBytes {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}
func TestLoadRejectsUnsafeContracts(t *testing.T) {
	tests := map[string]map[string]string{
		"short token":          {"MARSHALLER_API_TOKEN": "short", "MARSHALLER_VM_PASSWORD": "development-vm-password"},
		"wrong topic":          {"MARSHALLER_API_TOKEN": "marshaller-token-at-least-32-bytes-long", "MARSHALLER_VM_PASSWORD": "development-vm-password", "MARSHALLER_KAFKA_TOPIC": "other"},
		"wrong group":          {"MARSHALLER_API_TOKEN": "marshaller-token-at-least-32-bytes-long", "MARSHALLER_VM_PASSWORD": "development-vm-password", "MARSHALLER_KAFKA_GROUP": "other"},
		"credentials in URL":   {"MARSHALLER_API_TOKEN": "marshaller-token-at-least-32-bytes-long", "MARSHALLER_VM_PASSWORD": "development-vm-password", "MARSHALLER_VM_URL": "http://user:pass@127.0.0.1:8428"},
		"non loopback VM":      {"MARSHALLER_API_TOKEN": "marshaller-token-at-least-32-bytes-long", "MARSHALLER_VM_PASSWORD": "development-vm-password", "MARSHALLER_VM_URL": "http://192.0.2.1:8428"},
		"bad retry":            {"MARSHALLER_API_TOKEN": "marshaller-token-at-least-32-bytes-long", "MARSHALLER_VM_PASSWORD": "development-vm-password", "MARSHALLER_RETRY_MIN": "2s", "MARSHALLER_RETRY_MAX": "1s"},
		"bad in-flight":        {"MARSHALLER_API_TOKEN": "marshaller-token-at-least-32-bytes-long", "MARSHALLER_VM_PASSWORD": "development-vm-password", "MARSHALLER_MAX_IN_FLIGHT": "0"},
		"bad retry budget":     {"MARSHALLER_API_TOKEN": "marshaller-token-at-least-32-bytes-long", "MARSHALLER_VM_PASSWORD": "development-vm-password", "MARSHALLER_MAX_IN_FLIGHT": "2", "MARSHALLER_MAX_RETRYING": "3"},
		"bad partition budget": {"MARSHALLER_API_TOKEN": "marshaller-token-at-least-32-bytes-long", "MARSHALLER_VM_PASSWORD": "development-vm-password", "MARSHALLER_KAFKA_MIN_PARTITIONS": "0"},
	}
	for name, values := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := loadMap(values); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestLoadAcceptsTwoMarshallerMembersWithBoundedBudgets(t *testing.T) {
	values := validEnv()
	values["GOPULSE_INSTANCE_ID"] = "marshaller-2"
	values["GOPULSE_REPLICA_COUNT"] = "2"
	values["MARSHALLER_KAFKA_MIN_PARTITIONS"] = "4"
	values["MARSHALLER_MAX_IN_FLIGHT"] = "8"
	values["MARSHALLER_MAX_RETRYING"] = "4"
	cfg, err := loadMap(values)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReplicaCount != 2 || cfg.KafkaMinPartitions != 4 || cfg.MaxInFlight != 8 || cfg.MaxRetrying != 4 {
		t.Fatalf("unexpected bounded replica configuration: %+v", cfg)
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
