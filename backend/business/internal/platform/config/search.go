package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultElasticsearchURL     = "http://127.0.0.1:9200"
	defaultElasticsearchTimeout = 3 * time.Second
	defaultSearchReindexBatch   = 500
	minimumElasticsearchTimeout = 100 * time.Millisecond
	maximumElasticsearchTimeout = 30 * time.Second
	minimumSearchReindexBatch   = 1
	maximumSearchReindexBatch   = 5000
)

type ElasticsearchConfig struct {
	URL            string
	RequestTimeout time.Duration
	ReindexBatch   int
	Purpose        string
}

type ReindexConfig struct {
	MySQL                            MySQLConfig
	Elasticsearch                    ElasticsearchConfig
	BusinessAsyncMaxOpenRequirements uint64
	BusinessAsyncMaxRetainedGroups   uint64
	BusinessAsyncMaxRetainedBytes    uint64
}

// LoadReindex loads only the MySQL and Elasticsearch settings required by the
// role-isolated search rebuild command.
func LoadReindex() (ReindexConfig, error) {
	return LoadReindexFrom(os.LookupEnv)
}

func LoadReindexFrom(lookup LookupFunc) (ReindexConfig, error) {
	if lookup == nil {
		return ReindexConfig{}, errors.New("configuration lookup is required")
	}
	runtimeMode, err := loadRuntimeMode(lookup)
	if err != nil {
		return ReindexConfig{}, err
	}
	mysqlHost := valueOrDefault(lookup, "MYSQL_HOST", defaultMySQLHost)
	if err := validateDependencyHost(runtimeMode, "MYSQL_HOST", mysqlHost); err != nil {
		return ReindexConfig{}, err
	}
	mysqlPort, err := integerValue(lookup, "MYSQL_PORT", defaultMySQLPort)
	if err != nil {
		return ReindexConfig{}, err
	}
	if err := validatePort("MYSQL_PORT", mysqlPort); err != nil {
		return ReindexConfig{}, err
	}
	database, err := requiredValue(lookup, "MYSQL_DATABASE")
	if err != nil {
		return ReindexConfig{}, err
	}
	user, err := requiredValue(lookup, "MYSQL_USER")
	if err != nil {
		return ReindexConfig{}, err
	}
	password, err := requiredValue(lookup, "MYSQL_PASSWORD")
	if err != nil {
		return ReindexConfig{}, err
	}
	replicaCount, err := replicaCountValue(lookup)
	if err != nil {
		return ReindexConfig{}, err
	}
	maxOpenConns, maxIdleConns, connMaxLifetime, err := mysqlPoolValues(lookup, replicaCount)
	if err != nil {
		return ReindexConfig{}, err
	}
	elasticsearch, err := loadElasticsearchConfig(lookup, runtimeMode)
	if err != nil {
		return ReindexConfig{}, err
	}
	businessAsyncMaxOpenRequirements, err := businessAsyncMaxOpenRequirementsValue(lookup)
	if err != nil {
		return ReindexConfig{}, err
	}
	retainedGroups, retainedBytes, err := businessAsyncRetentionValues(lookup)
	if err != nil {
		return ReindexConfig{}, err
	}
	return ReindexConfig{
		MySQL: MySQLConfig{
			Host:            mysqlHost,
			Port:            mysqlPort,
			Database:        database,
			User:            user,
			Password:        password,
			MaxOpenConns:    maxOpenConns,
			MaxIdleConns:    maxIdleConns,
			ConnMaxLifetime: connMaxLifetime,
		},
		Elasticsearch:                    elasticsearch,
		BusinessAsyncMaxOpenRequirements: businessAsyncMaxOpenRequirements,
		BusinessAsyncMaxRetainedGroups:   retainedGroups,
		BusinessAsyncMaxRetainedBytes:    retainedBytes,
	}, nil
}

func loadElasticsearchConfig(lookup LookupFunc, runtimeMode RuntimeMode) (ElasticsearchConfig, error) {
	rawURL := valueOrDefault(lookup, "ELASTICSEARCH_URL", defaultElasticsearchURL)
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return ElasticsearchConfig{}, errors.New("ELASTICSEARCH_URL must be an HTTP(S) origin without userinfo")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return ElasticsearchConfig{}, errors.New("ELASTICSEARCH_URL must not include a query or fragment")
	}
	if err := validateOriginHost(runtimeMode, "ELASTICSEARCH_URL", parsed); err != nil {
		return ElasticsearchConfig{}, err
	}
	timeout, err := durationValue(lookup, "ELASTICSEARCH_REQUEST_TIMEOUT", defaultElasticsearchTimeout, minimumElasticsearchTimeout, maximumElasticsearchTimeout)
	if err != nil {
		return ElasticsearchConfig{}, err
	}
	batch, err := integerValue(lookup, "SEARCH_REINDEX_BATCH", defaultSearchReindexBatch)
	if err != nil {
		return ElasticsearchConfig{}, err
	}
	if batch < minimumSearchReindexBatch || batch > maximumSearchReindexBatch {
		return ElasticsearchConfig{}, fmt.Errorf("SEARCH_REINDEX_BATCH must be between %d and %d", minimumSearchReindexBatch, maximumSearchReindexBatch)
	}
	return ElasticsearchConfig{URL: strings.TrimRight(rawURL, "/"), RequestTimeout: timeout, ReindexBatch: batch, Purpose: "search"}, nil
}
