package runtime

import (
	"encoding/json"
	"strconv"

	"github.com/Ray-ymq/GoPulse/componentmetrics/exportercontract"
)

func LoadFile(path string) (Config, error) {
	envelope, err := exportercontract.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	fields, err := exportercontract.Object(envelope.Config)
	if err != nil || !only(fields, "host", "port", "topic", "consumer_group", "connect_timeout", "scrape_timeout", "jmx_url") {
		return Config{}, ErrConfig
	}
	secrets, err := exportercontract.Object(envelope.Secrets)
	if err != nil || len(secrets) != 0 {
		return Config{}, ErrConfig
	}
	host, err := exportercontract.String(fields, "host", true)
	if err != nil {
		return Config{}, ErrConfig
	}
	port, err := exportercontract.Integer(fields, "port", true)
	if err != nil {
		return Config{}, ErrConfig
	}
	topic, err := exportercontract.String(fields, "topic", true)
	if err != nil {
		return Config{}, ErrConfig
	}
	group, err := exportercontract.String(fields, "consumer_group", true)
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
	jmxURL, err := exportercontract.String(fields, "jmx_url", false)
	if err != nil {
		return Config{}, ErrConfig
	}
	values := exportercontract.RuntimeEnvironment("KAFKA", envelope.Runtime)
	values["KAFKA_HOST"], values["KAFKA_PORT"] = host, strconv.Itoa(port)
	values["KAFKA_TOPIC"], values["KAFKA_CONSUMER_GROUP"] = topic, group
	values["KAFKA_EXPORTER_CONNECT_TIMEOUT"], values["KAFKA_EXPORTER_SCRAPE_TIMEOUT"] = connect, scrape
	values["KAFKA_EXPORTER_JMX_URL"] = jmxURL
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
