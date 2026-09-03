package collector

import (
	"context"
	"errors"
	"math"
	"net"
	"strconv"
	"strings"

	goredis "github.com/redis/go-redis/v9"
)

type FailureReason string

const (
	ReasonRedisUnavailable     FailureReason = "redis_unavailable"
	ReasonRedisAuthentication  FailureReason = "redis_auth_failed"
	ReasonRedisTimeout         FailureReason = "redis_timeout"
	ReasonRedisResponseInvalid FailureReason = "redis_response_invalid"
)

type Failure struct{ Reason FailureReason }

func (f *Failure) Error() string { return string(f.Reason) }

type Snapshot struct {
	UptimeSeconds          uint64
	ConnectedClients       uint64
	UsedMemoryBytes        uint64
	CommandsProcessedTotal uint64
	KeyspaceHitsTotal      uint64
	KeyspaceMissesTotal    uint64
	CPUUserSeconds         float64
	CPUSystemSeconds       float64
	DB                     int
	DBKeys                 uint64
	DBExpiringKeys         uint64
}

type Collector interface {
	Collect(context.Context) (Snapshot, error)
}

type infoClient interface {
	Info(context.Context, ...string) *goredis.StringCmd
}

type RedisCollector struct {
	client infoClient
	db     int
}

func New(client infoClient, db int) *RedisCollector { return &RedisCollector{client: client, db: db} }

func (c *RedisCollector) Collect(ctx context.Context) (Snapshot, error) {
	info, err := c.client.Info(ctx, "server", "clients", "memory", "stats", "cpu", "keyspace").Result()
	if err != nil {
		return Snapshot{}, classifyRedisError(ctx, err)
	}
	snapshot, err := ParseInfo(info, c.db)
	if err != nil {
		return Snapshot{}, &Failure{Reason: ReasonRedisResponseInvalid}
	}
	return snapshot, nil
}

func Reason(err error) FailureReason {
	var failure *Failure
	if errors.As(err, &failure) {
		return failure.Reason
	}
	return ReasonRedisUnavailable
}

func classifyRedisError(ctx context.Context, err error) error {
	var networkError net.Error
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkError) && networkError.Timeout()) {
		return &Failure{Reason: ReasonRedisTimeout}
	}
	upper := strings.ToUpper(err.Error())
	if strings.Contains(upper, "WRONGPASS") || strings.Contains(upper, "NOAUTH") || strings.Contains(upper, "AUTHENTICATION") {
		return &Failure{Reason: ReasonRedisAuthentication}
	}
	return &Failure{Reason: ReasonRedisUnavailable}
}

func ParseInfo(info string, db int) (Snapshot, error) {
	if db < 0 {
		return Snapshot{}, errors.New("invalid database")
	}
	wanted := map[string]string{}
	required := map[string]struct{}{
		"uptime_in_seconds": {}, "connected_clients": {}, "used_memory": {},
		"total_commands_processed": {}, "keyspace_hits": {}, "keyspace_misses": {},
		"used_cpu_user": {}, "used_cpu_sys": {},
	}
	dbField := "db" + strconv.Itoa(db)
	var dbValue string
	dbSeen := false
	for _, raw := range strings.Split(strings.ReplaceAll(info, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok || name == "" {
			return Snapshot{}, errors.New("invalid info line")
		}
		if _, ok := required[name]; ok {
			if _, duplicate := wanted[name]; duplicate {
				return Snapshot{}, errors.New("duplicate field")
			}
			wanted[name] = value
		}
		if name == dbField {
			if dbSeen {
				return Snapshot{}, errors.New("duplicate database")
			}
			dbSeen, dbValue = true, value
		}
	}
	for name := range required {
		if _, ok := wanted[name]; !ok {
			return Snapshot{}, errors.New("missing field")
		}
	}
	var snapshot Snapshot
	var err error
	if snapshot.UptimeSeconds, err = parseUint(wanted["uptime_in_seconds"]); err != nil {
		return Snapshot{}, err
	}
	if snapshot.ConnectedClients, err = parseUint(wanted["connected_clients"]); err != nil {
		return Snapshot{}, err
	}
	if snapshot.UsedMemoryBytes, err = parseUint(wanted["used_memory"]); err != nil {
		return Snapshot{}, err
	}
	if snapshot.CommandsProcessedTotal, err = parseUint(wanted["total_commands_processed"]); err != nil {
		return Snapshot{}, err
	}
	if snapshot.KeyspaceHitsTotal, err = parseUint(wanted["keyspace_hits"]); err != nil {
		return Snapshot{}, err
	}
	if snapshot.KeyspaceMissesTotal, err = parseUint(wanted["keyspace_misses"]); err != nil {
		return Snapshot{}, err
	}
	if snapshot.CPUUserSeconds, err = parseFloat(wanted["used_cpu_user"]); err != nil {
		return Snapshot{}, err
	}
	if snapshot.CPUSystemSeconds, err = parseFloat(wanted["used_cpu_sys"]); err != nil {
		return Snapshot{}, err
	}
	snapshot.DB = db
	if dbSeen {
		if snapshot.DBKeys, snapshot.DBExpiringKeys, err = parseKeyspace(dbValue); err != nil {
			return Snapshot{}, err
		}
	}
	return snapshot, nil
}

func parseUint(value string) (uint64, error) {
	if strings.TrimSpace(value) != value || strings.HasPrefix(value, "-") {
		return 0, errors.New("invalid integer")
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, errors.New("invalid integer")
	}
	return parsed, nil
}
func parseFloat(value string) (float64, error) {
	if strings.TrimSpace(value) != value {
		return 0, errors.New("invalid float")
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 {
		return 0, errors.New("invalid float")
	}
	return parsed, nil
}
func parseKeyspace(value string) (uint64, uint64, error) {
	parts := strings.Split(value, ",")
	values := make(map[string]string, len(parts))
	for _, part := range parts {
		name, field, ok := strings.Cut(part, "=")
		if !ok || name == "" || field == "" {
			return 0, 0, errors.New("invalid keyspace")
		}
		if _, duplicate := values[name]; duplicate {
			return 0, 0, errors.New("duplicate keyspace field")
		}
		values[name] = field
	}
	keys, ok := values["keys"]
	if !ok {
		return 0, 0, errors.New("missing keys")
	}
	expires, ok := values["expires"]
	if !ok {
		return 0, 0, errors.New("missing expires")
	}
	parsedKeys, err := parseUint(keys)
	if err != nil {
		return 0, 0, err
	}
	parsedExpires, err := parseUint(expires)
	if err != nil {
		return 0, 0, err
	}
	return parsedKeys, parsedExpires, nil
}
