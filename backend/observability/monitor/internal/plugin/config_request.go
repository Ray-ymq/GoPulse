package plugin

import (
	"encoding/json"
)

// ParseRedisConfigurationRequest validates the public config/secrets boundary.
// previous must be nil for install and connection-test: only configuration
// replacement may retain the installed secret. This function has no side effects.
func ParseRedisConfigurationRequest(data []byte, mode string, previous *RedisSecret) (RedisConfig, RedisSecret, error) {
	fail := func() (RedisConfig, RedisSecret, error) { return RedisConfig{}, RedisSecret{}, invalidConfig() }
	if len(data) > 16<<10 || !uniqueJSON(data) {
		return fail()
	}
	var request map[string]json.RawMessage
	if json.Unmarshal(data, &request) != nil || request == nil {
		return fail()
	}
	for key := range request {
		if key != "config" && key != "secrets" {
			return fail()
		}
	}
	var config map[string]json.RawMessage
	if json.Unmarshal(request["config"], &config) != nil || config == nil {
		return fail()
	}
	if _, exists := config["password"]; exists {
		return fail()
	}
	secrets := map[string]json.RawMessage{}
	if raw, exists := request["secrets"]; exists {
		if json.Unmarshal(raw, &secrets) != nil || secrets == nil {
			return fail()
		}
	} else if previous == nil {
		return fail()
	}
	for key, raw := range secrets {
		if key != "password" {
			return fail()
		}
		config[key] = raw
	}
	combined, err := json.Marshal(config)
	if err != nil {
		return fail()
	}
	return ParseRedisConfiguration(combined, mode, previous)
}
