package config

import (
	"errors"
	"fmt"
	"os"
	"time"
)

const (
	defaultMySQLHost            = "127.0.0.1"
	defaultMySQLPort            = 3306
	defaultMySQLMaxOpenConns    = 10
	defaultMySQLMaxIdleConns    = 2
	defaultMySQLConnMaxLifetime = 3 * time.Minute
	defaultMySQLTotalOpenConns  = 60
	minimumMySQLMaxOpenConns    = 1
	maximumMySQLMaxOpenConns    = 100
	minimumMySQLConnLifetime    = time.Minute
	maximumMySQLConnLifetime    = time.Hour
	minimumMySQLTotalOpenConns  = 1
	maximumMySQLTotalOpenConns  = 1000
)

type MySQLConfig struct {
	Host            string
	Port            int
	Database        string
	User            string
	Password        string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

func mysqlPoolValues(lookup LookupFunc, replicaCount int) (int, int, time.Duration, error) {
	return mysqlPoolValuesWithKey(lookup, replicaCount, "MYSQL_MAX_OPEN_CONNS", defaultMySQLMaxOpenConns)
}

func mysqlPoolValuesForRole(lookup LookupFunc, replicaCount int, role ServiceRole) (int, int, time.Duration, error) {

	return mysqlPoolValues(lookup, replicaCount)
}

func mysqlPoolValuesWithKey(lookup LookupFunc, replicaCount int, maxOpenKey string, maxOpenDefault int) (int, int, time.Duration, error) {
	maxOpen, err := integerValue(lookup, maxOpenKey, maxOpenDefault)
	if err != nil || maxOpen < minimumMySQLMaxOpenConns || maxOpen > maximumMySQLMaxOpenConns {
		return 0, 0, 0, fmt.Errorf("%s must be between %d and %d", maxOpenKey, minimumMySQLMaxOpenConns, maximumMySQLMaxOpenConns)
	}
	maxIdle, err := integerValue(lookup, "MYSQL_MAX_IDLE_CONNS", defaultMySQLMaxIdleConns)
	if err != nil || maxIdle < 1 || maxIdle > maxOpen {
		return 0, 0, 0, errors.New("MYSQL_MAX_IDLE_CONNS must be between 1 and MYSQL_MAX_OPEN_CONNS")
	}
	lifetime, err := durationValue(lookup, "MYSQL_CONN_MAX_LIFETIME", defaultMySQLConnMaxLifetime, minimumMySQLConnLifetime, maximumMySQLConnLifetime)
	if err != nil {
		return 0, 0, 0, err
	}
	total, err := integerValue(lookup, "MYSQL_TOTAL_MAX_OPEN_CONNS", defaultMySQLTotalOpenConns)
	if err != nil || total < minimumMySQLTotalOpenConns || total > maximumMySQLTotalOpenConns {
		return 0, 0, 0, fmt.Errorf("MYSQL_TOTAL_MAX_OPEN_CONNS must be between %d and %d", minimumMySQLTotalOpenConns, maximumMySQLTotalOpenConns)
	}
	if maxOpen*replicaCount > total {
		return 0, 0, 0, errors.New("MYSQL_TOTAL_MAX_OPEN_CONNS must cover MYSQL_MAX_OPEN_CONNS times GOPULSE_REPLICA_COUNT")
	}
	return maxOpen, maxIdle, lifetime, nil
}

// LoadMySQL loads only the database settings required by role-isolated
// migration and administrator operations.
func LoadMySQL() (MySQLConfig, error) {
	return LoadMySQLFrom(os.LookupEnv)
}

func LoadMySQLFrom(lookup LookupFunc) (MySQLConfig, error) {
	if lookup == nil {
		return MySQLConfig{}, errors.New("configuration lookup is required")
	}
	runtimeMode, err := loadRuntimeMode(lookup)
	if err != nil {
		return MySQLConfig{}, err
	}
	host := valueOrDefault(lookup, "MYSQL_HOST", defaultMySQLHost)
	if err := validateDependencyHost(runtimeMode, "MYSQL_HOST", host); err != nil {
		return MySQLConfig{}, err
	}
	port, err := integerValue(lookup, "MYSQL_PORT", defaultMySQLPort)
	if err != nil {
		return MySQLConfig{}, err
	}
	if err := validatePort("MYSQL_PORT", port); err != nil {
		return MySQLConfig{}, err
	}
	database, err := requiredValue(lookup, "MYSQL_DATABASE")
	if err != nil {
		return MySQLConfig{}, err
	}
	user, err := requiredValue(lookup, "MYSQL_USER")
	if err != nil {
		return MySQLConfig{}, err
	}
	password, err := requiredValue(lookup, "MYSQL_PASSWORD")
	if err != nil {
		return MySQLConfig{}, err
	}
	replicaCount, err := replicaCountValue(lookup)
	if err != nil {
		return MySQLConfig{}, err
	}
	maxOpenConns, maxIdleConns, connMaxLifetime, err := mysqlPoolValues(lookup, replicaCount)
	if err != nil {
		return MySQLConfig{}, err
	}
	return MySQLConfig{
		Host:            host,
		Port:            port,
		Database:        database,
		User:            user,
		Password:        password,
		MaxOpenConns:    maxOpenConns,
		MaxIdleConns:    maxIdleConns,
		ConnMaxLifetime: connMaxLifetime,
	}, nil
}
