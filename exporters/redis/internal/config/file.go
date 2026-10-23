package config

import (
	"encoding/json"
	"strconv"

	"github.com/Ray-ymq/GoPulse/runtimekit/exportercontract"
)

// LoadFile is the independent runtime entry point. Source fields are decoded
// here; the process manager only hands over the opaque contract file.
func LoadFile(path string) (Config, error) {
	envelope, err := exportercontract.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	fields, err := exportercontract.Object(envelope.Config)
	if err != nil || !only(fields, "host", "port", "database", "connect_timeout", "scrape_timeout") {
		return Config{}, fieldError("config", "invalid_configuration")
	}
	secrets, err := exportercontract.Object(envelope.Secrets)
	if err != nil || !only(secrets, "password") {
		return Config{}, fieldError("secrets", "invalid_configuration")
	}
	host, err := exportercontract.String(fields, "host", true)
	if err != nil {
		return Config{}, err
	}
	port, err := exportercontract.Integer(fields, "port", true)
	if err != nil {
		return Config{}, err
	}
	database, err := exportercontract.Integer(fields, "database", true)
	if err != nil {
		return Config{}, err
	}
	connect, err := exportercontract.String(fields, "connect_timeout", true)
	if err != nil {
		return Config{}, err
	}
	scrape, err := exportercontract.String(fields, "scrape_timeout", true)
	if err != nil {
		return Config{}, err
	}
	password, err := exportercontract.String(secrets, "password", true)
	if err != nil {
		return Config{}, err
	}
	values := exportercontract.RuntimeEnvironment("REDIS", envelope.Runtime)
	values["REDIS_HOST"], values["REDIS_PORT"], values["REDIS_DB"] = host, strconv.Itoa(port), strconv.Itoa(database)
	values["REDIS_PASSWORD"], values["REDIS_EXPORTER_CONNECT_TIMEOUT"], values["REDIS_EXPORTER_SCRAPE_TIMEOUT"] = password, connect, scrape
	return exportercontract.LoadWithEnvironment(values, Load)
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
