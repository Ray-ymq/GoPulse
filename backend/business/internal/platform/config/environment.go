package config

import (
	"errors"
	"fmt"
	"strings"
)

const (
	defaultAppEnv       = "development"
	defaultHTTPHost     = "127.0.0.1"
	defaultHTTPPort     = 8080
	defaultReplicaCount = 1
	minimumReplicaCount = 1
	maximumReplicaCount = 8
)

func replicaCountValue(lookup LookupFunc) (int, error) {
	count, err := integerValue(lookup, "GOPULSE_REPLICA_COUNT", defaultReplicaCount)
	if err != nil || count < minimumReplicaCount || count > maximumReplicaCount {
		return 0, fmt.Errorf("GOPULSE_REPLICA_COUNT must be between %d and %d", minimumReplicaCount, maximumReplicaCount)
	}
	return count, nil
}

func applicationEnvironment(lookup LookupFunc) (string, error) {
	value := strings.ToLower(valueOrDefault(lookup, "APP_ENV", defaultAppEnv))
	switch value {
	case "development", "test", "production":
		return value, nil
	default:
		return "", errors.New("APP_ENV must be one of development, test, or production")
	}
}

func isLocalEnvironment(appEnv string) bool {
	return appEnv == "development" || appEnv == "test"
}
