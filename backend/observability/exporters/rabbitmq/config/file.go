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
	if err != nil || !only(fields, "host", "management_port", "vhost", "username", "connect_timeout", "scrape_timeout") {
		return Config{}, ErrConfig
	}
	secrets, err := object(envelope.Secrets)
	if err != nil || !only(secrets, "password") {
		return Config{}, ErrConfig
	}
	host, err := stringField(fields, "host", true)
	if err != nil {
		return Config{}, ErrConfig
	}
	port, err := integerField(fields, "management_port", true)
	if err != nil {
		return Config{}, ErrConfig
	}
	vhost, err := stringField(fields, "vhost", true)
	if err != nil {
		return Config{}, ErrConfig
	}
	username, err := stringField(fields, "username", true)
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
	password, err := stringField(secrets, "password", true)
	if err != nil {
		return Config{}, ErrConfig
	}
	values := runtimeEnvironment("RABBITMQ", envelope.Runtime)
	values["RABBITMQ_HOST"], values["RABBITMQ_MANAGEMENT_PORT"] = host, strconv.Itoa(port)
	values["RABBITMQ_VHOST"], values["RABBITMQ_USERNAME"], values["RABBITMQ_PASSWORD"] = vhost, username, password
	values["RABBITMQ_EXPORTER_CONNECT_TIMEOUT"], values["RABBITMQ_EXPORTER_SCRAPE_TIMEOUT"] = connect, scrape
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
