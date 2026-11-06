package collector

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestClusterSnapshotsAcceptNewMetricsAndDimensions(t *testing.T) {
	for _, source := range []string{"redis", "mysql", "rabbitmq", "kafka", "elasticsearch", "victoriametrics"} {
		t.Run(source, func(t *testing.T) {
			if _, err := New(Config{Source: source, Host: "127.0.0.1", Port: "9124", Interval: 2 * time.Second, Timeout: time.Second, PublishTimeout: time.Second}); err != nil {
				t.Fatal(err)
			}
			body := fmt.Sprintf("# TYPE gopulse_%s_up gauge\ngopulse_%s_up 1\n# TYPE gopulse_%s_new_depth gauge\ngopulse_%s_new_depth{node=\"node-2\",partition=\"3\"} 7\n", source, source, source, source)
			if err := ValidateSuccessfulSourceSnapshot(source, 200, []byte(body)); err != nil {
				t.Fatal(err)
			}
			other := "mysql"
			if source == other {
				other = "redis"
			}
			if err := ValidateSuccessfulSourceSnapshot(other, 200, []byte(body)); err == nil {
				t.Fatal("foreign namespace accepted")
			}
			if err := ValidateSuccessfulSourceSnapshot(source, 200, []byte(strings.Replace(body, "node=", "source=", 1))); err == nil {
				t.Fatal("reserved label accepted")
			}
		})
	}
}
