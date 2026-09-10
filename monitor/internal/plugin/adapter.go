package plugin

import (
	"encoding/json"
	"strconv"
)

// The transaction store knows only immutable JSON objects. Source adapters own
// field/origin validation and environment injection, not process or disk state.
// Later batches register adapters here without copying the lifecycle engine.
type configurationAdapter interface {
	Parse([]byte, string, json.RawMessage) (json.RawMessage, json.RawMessage, error)
	Environment(json.RawMessage, json.RawMessage) map[string]string
}
type redisAdapter struct{}

func adapterFor(id string) (configurationAdapter, bool) {
	if id == PluginID {
		return redisAdapter{}, true
	}
	return nil, false
}
func (redisAdapter) Parse(data []byte, mode string, previous json.RawMessage) (json.RawMessage, json.RawMessage, error) {
	var old *RedisSecret
	if previous != nil {
		old = &RedisSecret{}
		if json.Unmarshal(previous, old) != nil {
			return nil, nil, invalidConfig()
		}
	}
	config, secret, err := ParseRedisConfigurationRequest(data, mode, old)
	if err != nil {
		return nil, nil, err
	}
	public, _ := json.Marshal(config)
	private, _ := json.Marshal(secret)
	return public, private, nil
}
func (redisAdapter) Environment(public, private json.RawMessage) map[string]string {
	var cfg RedisConfig
	var secret RedisSecret
	if json.Unmarshal(public, &cfg) != nil || json.Unmarshal(private, &secret) != nil {
		return nil
	}
	return map[string]string{"REDIS_HOST": cfg.Host, "REDIS_PORT": strconv.Itoa(cfg.Port), "REDIS_DB": strconv.Itoa(cfg.Database), "REDIS_PASSWORD": secret.Password, "REDIS_EXPORTER_SCRAPE_TIMEOUT": cfg.ScrapeTimeout, "REDIS_EXPORTER_CONNECT_TIMEOUT": cfg.ConnectTimeout}
}
