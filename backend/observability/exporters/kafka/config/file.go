package config

import (
	"encoding/json"
	"strconv"
)

func LoadFile(path string) (Config, error) {
	envelope, err := readFile(path)
	if err != nil {
		return Config{}, err
	}
	fields, err := object(envelope.Config)
	if err != nil || !only(fields, "host", "port", "topic", "consumer_group", "connect_timeout", "scrape_timeout", "jmx_url") {
		return Config{}, ErrConfig
	}
	secrets, err := object(envelope.Secrets)
	if err != nil || len(secrets) != 0 {
		return Config{}, ErrConfig
	}
	host, err := stringField(fields, "host", true)
	if err != nil {
		return Config{}, ErrConfig
	}
	port, err := integerField(fields, "port", true)
	if err != nil {
		return Config{}, ErrConfig
	}
	topic, err := stringField(fields, "topic", true)
	if err != nil {
		return Config{}, ErrConfig
	}
	group, err := stringField(fields, "consumer_group", true)
	if err != nil {
		return Config{}, ErrConfig
	}
	connect, err := stringField(fields, "connect_timeout", true)
	if err != nil {
		return Config{}, ErrConfig
	}
	scrape, err := stringField(fields, "scrape_timeout", true)
	if err != nil {
		return Config{}, ErrConfig
	}
	jmxURL, err := stringField(fields, "jmx_url", false)
	if err != nil {
		return Config{}, ErrConfig
	}
	values := runtimeEnvironment("KAFKA", envelope.Runtime)
	values["KAFKA_HOST"], values["KAFKA_PORT"] = host, strconv.Itoa(port)
	values["KAFKA_TOPIC"], values["KAFKA_CONSUMER_GROUP"] = topic, group
	values["KAFKA_EXPORTER_CONNECT_TIMEOUT"], values["KAFKA_EXPORTER_SCRAPE_TIMEOUT"] = connect, scrape
	values["KAFKA_EXPORTER_JMX_URL"] = jmxURL
	return loadValues(values)
}

func only(fields map[string]json.RawMessage, names ...string) bool {
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		allowed[name] = true
	}
	for name := range fields {
		if !allowed[name] {
			return false
		}
	}
	return true
}
