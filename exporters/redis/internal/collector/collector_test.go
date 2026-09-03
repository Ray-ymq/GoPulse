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
# Memory
used_memory:4096
# Stats
total_commands_processed:45
keyspace_hits:12
keyspace_misses:2
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
	if snapshot.UptimeSeconds != 101 || snapshot.ConnectedClients != 3 || snapshot.UsedMemoryBytes != 4096 || snapshot.CommandsProcessedTotal != 45 || snapshot.KeyspaceHitsTotal != 12 || snapshot.KeyspaceMissesTotal != 2 || snapshot.CPUUserSeconds != 2.5 || snapshot.CPUSystemSeconds != 1.25 || snapshot.DB != 2 || snapshot.DBKeys != 7 || snapshot.DBExpiringKeys != 3 {
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
	if len(sections) != 6 {
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
