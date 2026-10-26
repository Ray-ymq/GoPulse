package runtime

import (
	"encoding/json"
	"strconv"

	"github.com/Ray-ymq/GoPulse/runtimekit/exportercontract"
)

func LoadFile(path string) (Config, error) {
	envelope, err := exportercontract.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	fields, err := exportercontract.Object(envelope.Config)
	if err != nil || !only(fields, "host", "management_port", "vhost", "username", "connect_timeout", "scrape_timeout") {
		return Config{}, ErrConfig
	}
	secrets, err := exportercontract.Object(envelope.Secrets)
	if err != nil || !only(secrets, "password") {
		return Config{}, ErrConfig
	}
	host, err := exportercontract.String(fields, "host", true)
	if err != nil {
		return Config{}, ErrConfig
	}
	port, err := exportercontract.Integer(fields, "management_port", true)
	if err != nil {
		return Config{}, ErrConfig
	}
	vhost, err := exportercontract.String(fields, "vhost", true)
	if err != nil {
		return Config{}, ErrConfig
	}
	username, err := exportercontract.String(fields, "username", true)
	if err != nil {
		return Config{}, ErrConfig
	}
	connect, err := exportercontract.String(fields, "connect_timeout", true)
	if err != nil {
		return Config{}, ErrConfig
	}
	scrape, err := exportercontract.String(fields, "scrape_timeout", true)
	if err != nil {
		return Config{}, ErrConfig
	}
	password, err := exportercontract.String(secrets, "password", true)
	if err != nil {
		return Config{}, ErrConfig
	}
	values := exportercontract.RuntimeEnvironment("RABBITMQ", envelope.Runtime)
	values["RABBITMQ_HOST"], values["RABBITMQ_MANAGEMENT_PORT"] = host, strconv.Itoa(port)
	values["RABBITMQ_VHOST"], values["RABBITMQ_USERNAME"], values["RABBITMQ_PASSWORD"] = vhost, username, password
	values["RABBITMQ_EXPORTER_CONNECT_TIMEOUT"], values["RABBITMQ_EXPORTER_SCRAPE_TIMEOUT"] = connect, scrape
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
