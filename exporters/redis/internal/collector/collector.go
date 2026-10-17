package collector

import (
	"context"
	"errors"
	"math"
	"net"
	"sort"
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
	MaxClients              *uint64
	UsedMemoryRSSBytes      *uint64
	MemoryFragmentation     *float64
	BlockedClients          *uint64
	RejectedConnections     *uint64
	EvictedKeysTotal        *uint64
	ExpiredKeysTotal        *uint64
	NetworkInputBytesTotal  *uint64
	NetworkOutputBytesTotal *uint64
	CommandStats            map[string]CommandStats
	Replication             ReplicationSnapshot
	Persistence             PersistenceSnapshot
}

type ReplicationSnapshot struct {
	Role                   string
	ConnectedReplicas      *uint64
	MasterReplOffset       *uint64
	MasterLastIOSecondsAgo *uint64
	MasterLinkUp           *bool
	SyncInProgress         *bool
	Replicas               []ReplicaSnapshot
}

type ReplicaSnapshot struct {
	Index      int
	State      string
	Offset     uint64
	LagSeconds uint64
	LinkUp     bool
}

type PersistenceSnapshot struct {
	RDBChangesSinceLastSave   *uint64
	RDBBGSAVEInProgress       *bool
	RDBLastSaveTimestamp      *uint64
	RDBLastBGSAVESuccess      *bool
	RDBCurrentBGSAVEDuration  *uint64
	AOFEnabled                *bool
	AOFRewriteInProgress      *bool
	AOFRewriteScheduled       *bool
	AOFLastRewriteSuccess     *bool
	AOFCurrentRewriteDuration *uint64
	AOFLastWriteSuccess       *bool
	AOFRewritesTotal          *uint64
	AOFRewriteFailuresTotal   *uint64
	AOFBaseSizeBytes          *uint64
	AOFCurrentSizeBytes       *uint64
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

const maximumReplicas = 32

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
	info, err := c.client.Info(ctx, "server", "clients", "memory", "stats", "cpu", "keyspace", "commandstats", "replication", "persistence").Result()
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
	replicas := map[int]ReplicaSnapshot{}
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
		if index, ok := replicaIndex(name); ok {
			if _, duplicate := replicas[index]; duplicate {
				return Snapshot{}, errors.New("duplicate replica")
			}
			replica, err := parseReplica(value, index)
			if err != nil {
				return Snapshot{}, err
			}
			replicas[index] = replica
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
	if snapshot.MaxClients, err = optionalUint(wanted, "maxclients"); err != nil {
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
	snapshot.Replication, err = parseReplication(wanted, replicas)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.Persistence, err = parsePersistence(wanted)
	if err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

var optionalInfoFields = map[string]struct{}{
	"maxmemory": {}, "maxclients": {}, "used_memory_rss": {}, "mem_fragmentation_ratio": {},
	"blocked_clients": {}, "rejected_connections": {}, "evicted_keys": {}, "expired_keys": {},
	"total_net_input_bytes": {}, "total_net_output_bytes": {},
	"role": {}, "connected_slaves": {}, "master_repl_offset": {}, "master_last_io_seconds_ago": {},
	"master_link_status": {}, "master_sync_in_progress": {},
	"rdb_changes_since_last_save": {}, "rdb_bgsave_in_progress": {}, "rdb_last_save_time": {},
	"rdb_last_bgsave_status": {}, "rdb_current_bgsave_time_sec": {},
	"aof_enabled": {}, "aof_rewrite_in_progress": {}, "aof_rewrite_scheduled": {},
	"aof_last_rewrite_status": {}, "aof_current_rewrite_time_sec": {}, "aof_last_write_status": {},
	"aof_rewrites": {}, "aof_rewrites_consecutive_failures": {}, "aof_base_size": {}, "aof_current_size": {},
}

func replicaIndex(name string) (int, bool) {
	prefix := ""
	for _, candidate := range []string{"slave", "replica"} {
		if strings.HasPrefix(name, candidate) {
			prefix = candidate
			break
		}
	}
	if prefix == "" || len(name) == len(prefix) {
		return 0, false
	}
	value := name[len(prefix):]
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, false
		}
	}
	index, err := strconv.Atoi(value)
	if err != nil || index < 0 || index >= maximumReplicas {
		return 0, false
	}
	return index, true
}

func parseReplica(value string, index int) (ReplicaSnapshot, error) {
	fields, err := keyValueFields(value)
	if err != nil {
		return ReplicaSnapshot{}, err
	}
	state, ok := fields["state"]
	if !ok || (state != "online" && state != "wait_bgsave" && state != "wait_bgsync") {
		return ReplicaSnapshot{}, errors.New("invalid replica state")
	}
	offset, err := parseUint(fields["offset"])
	if err != nil {
		return ReplicaSnapshot{}, err
	}
	lag, err := parseUint(fields["lag"])
	if err != nil {
		return ReplicaSnapshot{}, err
	}
	return ReplicaSnapshot{Index: index, State: state, Offset: offset, LagSeconds: lag, LinkUp: state == "online"}, nil
}

func parseReplication(values map[string]string, replicas map[int]ReplicaSnapshot) (ReplicationSnapshot, error) {
	var snapshot ReplicationSnapshot
	var err error
	if role, ok := values["role"]; ok {
		switch role {
		case "master":
			snapshot.Role = role
		case "slave", "replica":
			snapshot.Role = "replica"
		default:
			return ReplicationSnapshot{}, errors.New("invalid replication role")
		}
	}
	if snapshot.ConnectedReplicas, err = optionalUint(values, "connected_slaves"); err != nil {
		return ReplicationSnapshot{}, err
	}
	if snapshot.MasterReplOffset, err = optionalUint(values, "master_repl_offset"); err != nil {
		return ReplicationSnapshot{}, err
	}
	if snapshot.MasterLastIOSecondsAgo, err = optionalDuration(values, "master_last_io_seconds_ago"); err != nil {
		return ReplicationSnapshot{}, err
	}
	if snapshot.MasterLinkUp, err = optionalStatus(values, "master_link_status"); err != nil {
		return ReplicationSnapshot{}, err
	}
	if snapshot.SyncInProgress, err = optionalBoolean(values, "master_sync_in_progress"); err != nil {
		return ReplicationSnapshot{}, err
	}
	indices := make([]int, 0, len(replicas))
	for index := range replicas {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		snapshot.Replicas = append(snapshot.Replicas, replicas[index])
	}
	return snapshot, nil
}

func parsePersistence(values map[string]string) (PersistenceSnapshot, error) {
	var snapshot PersistenceSnapshot
	var err error
	if snapshot.RDBChangesSinceLastSave, err = optionalUint(values, "rdb_changes_since_last_save"); err != nil {
		return PersistenceSnapshot{}, err
	}
	if snapshot.RDBBGSAVEInProgress, err = optionalBoolean(values, "rdb_bgsave_in_progress"); err != nil {
		return PersistenceSnapshot{}, err
	}
	if snapshot.RDBLastSaveTimestamp, err = optionalUint(values, "rdb_last_save_time"); err != nil {
		return PersistenceSnapshot{}, err
	}
	if snapshot.RDBLastBGSAVESuccess, err = optionalStatus(values, "rdb_last_bgsave_status"); err != nil {
		return PersistenceSnapshot{}, err
	}
	if snapshot.RDBCurrentBGSAVEDuration, err = optionalDuration(values, "rdb_current_bgsave_time_sec"); err != nil {
		return PersistenceSnapshot{}, err
	}
	if snapshot.AOFEnabled, err = optionalBoolean(values, "aof_enabled"); err != nil {
		return PersistenceSnapshot{}, err
	}
	if snapshot.AOFEnabled == nil || !*snapshot.AOFEnabled {
		return snapshot, nil
	}
	if snapshot.AOFRewriteInProgress, err = optionalBoolean(values, "aof_rewrite_in_progress"); err != nil {
		return PersistenceSnapshot{}, err
	}
	if snapshot.AOFRewriteScheduled, err = optionalBoolean(values, "aof_rewrite_scheduled"); err != nil {
		return PersistenceSnapshot{}, err
	}
	if snapshot.AOFLastRewriteSuccess, err = optionalStatus(values, "aof_last_rewrite_status"); err != nil {
		return PersistenceSnapshot{}, err
	}
	if snapshot.AOFCurrentRewriteDuration, err = optionalDuration(values, "aof_current_rewrite_time_sec"); err != nil {
		return PersistenceSnapshot{}, err
	}
	if snapshot.AOFLastWriteSuccess, err = optionalStatus(values, "aof_last_write_status"); err != nil {
		return PersistenceSnapshot{}, err
	}
	if snapshot.AOFRewritesTotal, err = optionalUint(values, "aof_rewrites"); err != nil {
		return PersistenceSnapshot{}, err
	}
	if snapshot.AOFRewriteFailuresTotal, err = optionalUint(values, "aof_rewrites_consecutive_failures"); err != nil {
		return PersistenceSnapshot{}, err
	}
	if snapshot.AOFBaseSizeBytes, err = optionalUint(values, "aof_base_size"); err != nil {
		return PersistenceSnapshot{}, err
	}
	if snapshot.AOFCurrentSizeBytes, err = optionalUint(values, "aof_current_size"); err != nil {
		return PersistenceSnapshot{}, err
	}
	return snapshot, nil
}

func keyValueFields(value string) (map[string]string, error) {
	parts := strings.Split(value, ",")
	fields := make(map[string]string, len(parts))
	for _, part := range parts {
		key, field, ok := strings.Cut(part, "=")
		if !ok || key == "" || field == "" {
			return nil, errors.New("invalid info field")
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, errors.New("duplicate info field")
		}
		fields[key] = field
	}
	return fields, nil
}

func optionalBoolean(values map[string]string, name string) (*bool, error) {
	value, ok := values[name]
	if !ok {
		return nil, nil
	}
	if value != "0" && value != "1" {
		return nil, errors.New("invalid boolean")
	}
	parsed := value == "1"
	return &parsed, nil
}

func optionalStatus(values map[string]string, name string) (*bool, error) {
	value, ok := values[name]
	if !ok {
		return nil, nil
	}
	var parsed bool
	switch value {
	case "ok", "success", "online", "up":
		parsed = true
	case "err", "fail", "failed", "down":
		parsed = false
	default:
		return nil, errors.New("invalid status")
	}
	return &parsed, nil
}

func optionalDuration(values map[string]string, name string) (*uint64, error) {
	value, ok := values[name]
	if !ok || value == "-1" {
		return nil, nil
	}
	return optionalUint(values, name)
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
