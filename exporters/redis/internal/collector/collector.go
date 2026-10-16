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
	UptimeSeconds           uint64
	ConnectedClients        uint64
	UsedMemoryBytes         uint64
	CommandsProcessedTotal  uint64
	KeyspaceHitsTotal       uint64
	KeyspaceMissesTotal     uint64
	CPUUserSeconds          float64
	CPUSystemSeconds        float64
	DB                      int
	DBKeys                  uint64
	DBExpiringKeys          uint64
	MaxMemoryBytes          *uint64
	UsedMemoryRSSBytes      *uint64
	MemoryFragmentation     *float64
	BlockedClients          *uint64
	RejectedConnections     *uint64
	EvictedKeysTotal        *uint64
	ExpiredKeysTotal        *uint64
	NetworkInputBytesTotal  *uint64
	NetworkOutputBytesTotal *uint64
	CommandStats            map[string]CommandStats
}

type CommandStats struct {
	CallsTotal        uint64
	DurationUsecTotal uint64
	RejectedTotal     uint64
	FailedTotal       uint64
}

// TrackedCommands is deliberately finite. Redis exposes one commandstats
// entry per command name, so exporting the complete section would make label
// cardinality depend on application traffic.
var TrackedCommands = []string{
	"append", "del", "exists", "expire", "get", "hget", "hset", "incr", "lpop", "lpush",
	"mget", "mset", "rpop", "rpush", "sadd", "srem", "set", "smembers", "zadd", "zrange",
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
	info, err := c.client.Info(ctx, "server", "clients", "memory", "stats", "cpu", "keyspace", "commandstats").Result()
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
	commandStats := map[string]CommandStats{}
	tracked := make(map[string]bool, len(TrackedCommands))
	for _, command := range TrackedCommands {
		tracked[command] = true
	}
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
		if _, ok := optionalInfoFields[name]; ok {
			if _, duplicate := wanted[name]; duplicate {
				return Snapshot{}, errors.New("duplicate field")
			}
			wanted[name] = value
		}
		if strings.HasPrefix(name, "cmdstat_") {
			command := strings.TrimPrefix(name, "cmdstat_")
			if tracked[command] {
				if _, duplicate := commandStats[command]; duplicate {
					return Snapshot{}, errors.New("duplicate commandstats field")
				}
				stats, err := parseCommandStats(value)
				if err != nil {
					return Snapshot{}, err
				}
				commandStats[command] = stats
			}
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
	if snapshot.MaxMemoryBytes, err = optionalUint(wanted, "maxmemory"); err != nil {
		return Snapshot{}, err
	}
	if snapshot.UsedMemoryRSSBytes, err = optionalUint(wanted, "used_memory_rss"); err != nil {
		return Snapshot{}, err
	}
	if snapshot.MemoryFragmentation, err = optionalFloat(wanted, "mem_fragmentation_ratio"); err != nil {
		return Snapshot{}, err
	}
	if snapshot.BlockedClients, err = optionalUint(wanted, "blocked_clients"); err != nil {
		return Snapshot{}, err
	}
	if snapshot.RejectedConnections, err = optionalUint(wanted, "rejected_connections"); err != nil {
		return Snapshot{}, err
	}
	if snapshot.EvictedKeysTotal, err = optionalUint(wanted, "evicted_keys"); err != nil {
		return Snapshot{}, err
	}
	if snapshot.ExpiredKeysTotal, err = optionalUint(wanted, "expired_keys"); err != nil {
		return Snapshot{}, err
	}
	if snapshot.NetworkInputBytesTotal, err = optionalUint(wanted, "total_net_input_bytes"); err != nil {
		return Snapshot{}, err
	}
	if snapshot.NetworkOutputBytesTotal, err = optionalUint(wanted, "total_net_output_bytes"); err != nil {
		return Snapshot{}, err
	}
	if len(commandStats) > 0 {
		snapshot.CommandStats = commandStats
	}
	return snapshot, nil
}

var optionalInfoFields = map[string]struct{}{
	"maxmemory": {}, "used_memory_rss": {}, "mem_fragmentation_ratio": {},
	"blocked_clients": {}, "rejected_connections": {}, "evicted_keys": {}, "expired_keys": {},
	"total_net_input_bytes": {}, "total_net_output_bytes": {},
}

func parseCommandStats(value string) (CommandStats, error) {
	parts := strings.Split(value, ",")
	fields := make(map[string]string, len(parts))
	for _, part := range parts {
		key, field, ok := strings.Cut(part, "=")
		if !ok || key == "" || field == "" {
			return CommandStats{}, errors.New("invalid commandstats field")
		}
		if _, duplicate := fields[key]; duplicate {
			return CommandStats{}, errors.New("duplicate commandstats value")
		}
		fields[key] = field
	}
	parse := func(name string) (uint64, error) {
		value, ok := fields[name]
		if !ok {
			return 0, errors.New("missing commandstats value")
		}
		return parseUint(value)
	}
	calls, err := parse("calls")
	if err != nil {
		return CommandStats{}, err
	}
	usec, err := parse("usec")
	if err != nil {
		return CommandStats{}, err
	}
	rejected, err := parse("rejected_calls")
	if err != nil {
		return CommandStats{}, err
	}
	failed, err := parse("failed_calls")
	if err != nil {
		return CommandStats{}, err
	}
	return CommandStats{CallsTotal: calls, DurationUsecTotal: usec, RejectedTotal: rejected, FailedTotal: failed}, nil
}

func optionalUint(values map[string]string, name string) (*uint64, error) {
	value, ok := values[name]
	if !ok {
		return nil, nil
	}
	parsed, err := parseUint(value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func optionalFloat(values map[string]string, name string) (*float64, error) {
	value, ok := values[name]
	if !ok {
		return nil, nil
	}
	parsed, err := parseFloat(value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
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
