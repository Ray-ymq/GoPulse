package collector

import (
	"context"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

const validInfo = `# Server
uptime_in_seconds:101
# Clients
connected_clients:3
maxclients:100
# Memory
used_memory:4096
used_memory_rss:6144
maxmemory:16384
mem_fragmentation_ratio:1.5
# Stats
total_commands_processed:45
keyspace_hits:12
keyspace_misses:2
blocked_clients:1
rejected_connections:4
evicted_keys:5
expired_keys:6
total_net_input_bytes:700
total_net_output_bytes:800
cmdstat_get:calls=10,usec=100,rejected_calls=1,failed_calls=2
# CPU
used_cpu_sys:1.25
used_cpu_user:2.5
# Keyspace
db2:keys=7,expires=3,avg_ttl=1000
`

func TestParseInfoCreatesCompleteSnapshotAndDefaultsMissingDB(t *testing.T) {
	snapshot, err := ParseInfo(validInfo, 2)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.UptimeSeconds != 101 || snapshot.ConnectedClients != 3 || snapshot.UsedMemoryBytes != 4096 || snapshot.CommandsProcessedTotal != 45 || snapshot.KeyspaceHitsTotal != 12 || snapshot.KeyspaceMissesTotal != 2 || snapshot.CPUUserSeconds != 2.5 || snapshot.CPUSystemSeconds != 1.25 || snapshot.DB != 2 || snapshot.DBKeys != 7 || snapshot.DBExpiringKeys != 3 || snapshot.MaxMemoryBytes == nil || *snapshot.MaxMemoryBytes != 16384 || snapshot.MaxClients == nil || *snapshot.MaxClients != 100 || snapshot.UsedMemoryRSSBytes == nil || *snapshot.UsedMemoryRSSBytes != 6144 || snapshot.MemoryFragmentation == nil || *snapshot.MemoryFragmentation != 1.5 || snapshot.CommandStats["get"].CallsTotal != 10 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	missing, err := ParseInfo(validInfo, 9)
	if err != nil {
		t.Fatal(err)
	}
	if missing.DBKeys != 0 || missing.DBExpiringKeys != 0 {
		t.Fatalf("missing DB should be zero: %#v", missing)
	}
}

func TestParseInfoAddsReplicationAndPersistenceWithoutAOFPlaceholders(t *testing.T) {
	info := validInfo + `# Replication
role:master
connected_slaves:1
master_repl_offset:44
master_last_io_seconds_ago:-1
master_link_status:up
master_sync_in_progress:0
slave0:ip=127.0.0.1,port=6379,state=online,offset=42,lag=1
# Persistence
rdb_changes_since_last_save:8
rdb_bgsave_in_progress:0
rdb_last_save_time:1700000000
rdb_last_bgsave_status:ok
rdb_current_bgsave_time_sec:-1
aof_enabled:0
`
	snapshot, err := ParseInfo(info, 2)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Replication.Role != "master" || snapshot.Replication.ConnectedReplicas == nil || *snapshot.Replication.ConnectedReplicas != 1 || snapshot.Replication.MasterLastIOSecondsAgo != nil || snapshot.Replication.MasterLinkUp == nil || !*snapshot.Replication.MasterLinkUp || len(snapshot.Replication.Replicas) != 1 || !snapshot.Replication.Replicas[0].LinkUp || snapshot.Persistence.RDBLastSaveTimestamp == nil || *snapshot.Persistence.RDBLastSaveTimestamp != 1700000000 {
		t.Fatalf("unexpected second batch snapshot: %#v", snapshot)
	}
	if snapshot.Persistence.AOFEnabled == nil || *snapshot.Persistence.AOFEnabled || snapshot.Persistence.AOFCurrentSizeBytes != nil {
		t.Fatalf("disabled AOF must remain not applicable: %#v", snapshot.Persistence)
	}
}

func TestParseInfoAddsEnabledAOFDetails(t *testing.T) {
	info := validInfo + `# Persistence
aof_enabled:1
aof_rewrite_in_progress:0
aof_rewrite_scheduled:1
aof_last_rewrite_status:ok
aof_current_rewrite_time_sec:2
aof_last_write_status:ok
aof_rewrites:4
aof_rewrites_consecutive_failures:1
aof_base_size:100
aof_current_size:120
`
	snapshot, err := ParseInfo(info, 0)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Persistence.AOFEnabled == nil || !*snapshot.Persistence.AOFEnabled || snapshot.Persistence.AOFRewriteInProgress == nil || *snapshot.Persistence.AOFRewriteInProgress || snapshot.Persistence.AOFRewriteScheduled == nil || !*snapshot.Persistence.AOFRewriteScheduled || snapshot.Persistence.AOFLastRewriteSuccess == nil || !*snapshot.Persistence.AOFLastRewriteSuccess || snapshot.Persistence.AOFCurrentRewriteDuration == nil || *snapshot.Persistence.AOFCurrentRewriteDuration != 2 || snapshot.Persistence.AOFRewritesTotal == nil || *snapshot.Persistence.AOFRewritesTotal != 4 || snapshot.Persistence.AOFRewriteFailuresTotal == nil || *snapshot.Persistence.AOFRewriteFailuresTotal != 1 || snapshot.Persistence.AOFBaseSizeBytes == nil || *snapshot.Persistence.AOFBaseSizeBytes != 100 || snapshot.Persistence.AOFCurrentSizeBytes == nil || *snapshot.Persistence.AOFCurrentSizeBytes != 120 {
		t.Fatalf("unexpected enabled AOF snapshot: %#v", snapshot.Persistence)
	}
}

func TestParseInfoRejectsInvalidCompleteSnapshot(t *testing.T) {
	cases := []string{
		"uptime_in_seconds:101\n", // missing required fields
		validInfo + "keyspace_hits:13\n",
		stringReplace(validInfo, "keyspace_hits:12", "keyspace_hits:-1"),
		stringReplace(validInfo, "used_cpu_user:2.5", "used_cpu_user:NaN"),
		stringReplace(validInfo, "used_memory:4096", "used_memory:18446744073709551616"),
		stringReplace(validInfo, "db2:keys=7,expires=3,avg_ttl=1000", "db2:keys=7,avg_ttl=1000"),
	}
	for index, input := range cases {
		if _, err := ParseInfo(input, 2); err == nil {
			t.Fatalf("case %d unexpectedly succeeded", index)
		}
	}
}

func stringReplace(value, old, replacement string) string {
	for i := 0; i+len(old) <= len(value); i++ {
		if value[i:i+len(old)] == old {
			return value[:i] + replacement + value[i+len(old):]
		}
	}
	return value
}

type fakeInfoClient struct {
	calls  int
	result string
	err    error
}

func (f *fakeInfoClient) Info(_ context.Context, sections ...string) *goredis.StringCmd {
	f.calls++
	if len(sections) != 9 {
		return goredis.NewStringResult("", context.Canceled)
	}
	return goredis.NewStringResult(f.result, f.err)
}

func TestRedisCollectorCollectsExactlyOneInfoSnapshot(t *testing.T) {
	client := &fakeInfoClient{result: validInfo}
	snapshot, err := New(client, 2).Collect(context.Background())
	if err != nil || client.calls != 1 || snapshot.DBKeys != 7 {
		t.Fatalf("snapshot=%#v calls=%d err=%v", snapshot, client.calls, err)
	}
}
