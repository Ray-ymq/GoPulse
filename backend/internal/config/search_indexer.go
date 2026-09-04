package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

type SearchIndexerConfig struct {
	MySQL         MySQLConfig
	RabbitMQURL   string
	Elasticsearch ElasticsearchConfig
	Worker        BusinessWorkerConfig
	LogShip       LogShipConfig
}

func LoadSearchIndexer() (SearchIndexerConfig, error) {
	return LoadSearchIndexerFrom(os.LookupEnv)
}

func LoadSearchIndexerFrom(lookup LookupFunc) (SearchIndexerConfig, error) {
	if lookup == nil {
		return SearchIndexerConfig{}, errors.New("configuration lookup is required")
	}
	mysqlPort, err := integerValue(lookup, "MYSQL_PORT", defaultMySQLPort)
	if err != nil {
		return SearchIndexerConfig{}, err
	}
	if err := validatePort("MYSQL_PORT", mysqlPort); err != nil {
		return SearchIndexerConfig{}, err
	}
	database, err := requiredValue(lookup, "MYSQL_DATABASE")
	if err != nil {
		return SearchIndexerConfig{}, err
	}
	user, err := requiredValue(lookup, "MYSQL_USER")
	if err != nil {
		return SearchIndexerConfig{}, err
	}
	password, err := requiredValue(lookup, "MYSQL_PASSWORD")
	if err != nil {
		return SearchIndexerConfig{}, err
	}
	rabbitMQURL, err := requiredValue(lookup, "RABBITMQ_URL")
	if err != nil {
		return SearchIndexerConfig{}, err
	}
	if err := validateRabbitMQURL(rabbitMQURL); err != nil {
		return SearchIndexerConfig{}, err
	}
	elasticsearch, err := loadSearchIndexerElasticsearch(lookup)
	if err != nil {
		return SearchIndexerConfig{}, err
	}
	prefetch, err := integerValue(lookup, "SEARCH_INDEXER_PREFETCH", defaultWorkerPrefetch)
	if err != nil || prefetch < minimumWorkerPrefetch || prefetch > maximumWorkerPrefetch {
		return SearchIndexerConfig{}, fmt.Errorf("SEARCH_INDEXER_PREFETCH must be an integer between %d and %d", minimumWorkerPrefetch, maximumWorkerPrefetch)
	}
	maxRetries, err := integerValue(lookup, "SEARCH_INDEXER_MAX_RETRIES", defaultWorkerMaxRetries)
	if err != nil || maxRetries < minimumWorkerMaxRetries || maxRetries > maximumWorkerMaxRetries {
		return SearchIndexerConfig{}, fmt.Errorf("SEARCH_INDEXER_MAX_RETRIES must be an integer between %d and %d", minimumWorkerMaxRetries, maximumWorkerMaxRetries)
	}
	retryDelay, err := durationValue(lookup, "SEARCH_INDEXER_RETRY_DELAY", defaultOutboxRetryDelay, minimumOutboxRetryDelay, maximumOutboxRetryDelay)
	if err != nil {
		return SearchIndexerConfig{}, err
	}
	publishTimeout, err := durationValue(lookup, "SEARCH_INDEXER_PUBLISH_TIMEOUT", defaultWorkerPublishTimeout, minimumOutboxPublishTimeout, maximumOutboxPublishTimeout)
	if err != nil {
		return SearchIndexerConfig{}, err
	}
	shutdownTimeout, err := durationValue(lookup, "SEARCH_INDEXER_SHUTDOWN_TIMEOUT", defaultWorkerShutdownTimeout, minimumWorkerShutdownTimeout, maximumWorkerShutdownTimeout)
	if err != nil {
		return SearchIndexerConfig{}, err
	}
	reconnectMinimum, err := durationValue(lookup, "SEARCH_INDEXER_RECONNECT_MIN", defaultWorkerReconnectMinimum, minimumWorkerReconnectDuration, maximumWorkerReconnectDuration)
	if err != nil {
		return SearchIndexerConfig{}, err
	}
	reconnectMaximum, err := durationValue(lookup, "SEARCH_INDEXER_RECONNECT_MAX", defaultWorkerReconnectMaximum, minimumWorkerReconnectDuration, maximumWorkerReconnectDuration)
	if err != nil {
		return SearchIndexerConfig{}, err
	}
	if reconnectMaximum < reconnectMinimum {
		return SearchIndexerConfig{}, errors.New("SEARCH_INDEXER_RECONNECT_MAX must be greater than or equal to SEARCH_INDEXER_RECONNECT_MIN")
	}
	logShip, err := loadLogShipConfig(lookup)
	if err != nil {
		return SearchIndexerConfig{}, err
	}
	return SearchIndexerConfig{
		MySQL:         MySQLConfig{Host: valueOrDefault(lookup, "MYSQL_HOST", defaultMySQLHost), Port: mysqlPort, Database: database, User: user, Password: password},
		RabbitMQURL:   rabbitMQURL,
		Elasticsearch: elasticsearch,
		LogShip:       logShip,
		Worker:        BusinessWorkerConfig{Prefetch: prefetch, MaxRetries: maxRetries, RetryDelay: retryDelay, PublishTimeout: publishTimeout, ShutdownTimeout: shutdownTimeout, ReconnectMinimum: reconnectMinimum, ReconnectMaximum: reconnectMaximum},
	}, nil
}

func loadSearchIndexerElasticsearch(lookup LookupFunc) (ElasticsearchConfig, error) {
	rawURL := valueOrDefault(lookup, "ELASTICSEARCH_URL", defaultElasticsearchURL)
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ElasticsearchConfig{}, errors.New("ELASTICSEARCH_URL must be an HTTP(S) URL without userinfo, query, or fragment")
	}
	timeout, err := durationValue(lookup, "ELASTICSEARCH_REQUEST_TIMEOUT", defaultElasticsearchTimeout, minimumElasticsearchTimeout, maximumElasticsearchTimeout)
	if err != nil {
		return ElasticsearchConfig{}, err
	}
	return ElasticsearchConfig{URL: strings.TrimRight(rawURL, "/"), RequestTimeout: timeout, ReindexBatch: defaultSearchReindexBatch}, nil
}
