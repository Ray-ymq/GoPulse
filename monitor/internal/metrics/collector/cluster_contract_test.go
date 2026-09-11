package collector

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestClusterContracts(t *testing.T) {
	for _, source := range []string{"mysql", "rabbitmq", "kafka", "elasticsearch"} {
		t.Run(source, func(t *testing.T) {
			if _, err := New(Config{Source: source, Host: "127.0.0.1", Port: "9124", Interval: 2 * time.Second, Timeout: time.Second, PublishTimeout: time.Second}); err != nil {
				t.Fatal(err)
			}
			var b strings.Builder
			for name, c := range contractsFor(source) {
				fmt.Fprintf(&b, "# TYPE %s %s\n", name, strings.ToLower(c.kind.String()))
				if source == "elasticsearch" && c.count == 3 {
					fmt.Fprintf(&b, "%s{status=\"green\"} 0\n%s{status=\"yellow\"} 1\n%s{status=\"red\"} 0\n", name, name, name)
				} else if c.count == 1 {
					fmt.Fprintf(&b, "%s 1\n", name)
				} else {
					label, values := "result", []string{"commit", "rollback"}
					if source == "rabbitmq" {
						label, values = "state", []string{"ready", "unacked"}
					}
					for _, value := range values {
						fmt.Fprintf(&b, "%s{%s=\"%s\"} 1\n", name, label, value)
					}
				}
			}
			if err := ValidateSuccessfulSourceSnapshot(source, 200, []byte(b.String())); err != nil {
				t.Fatal(err)
			}
			if source == "elasticsearch" {
				if err := ValidateSuccessfulSourceSnapshot(source, 200, []byte(strings.Replace(b.String(), `status="green"} 0`, `status="green"} 1`, 1))); err == nil {
					t.Fatal("non-one-hot health accepted")
				}
			}
			other := "mysql"
			if source == other {
				other = "rabbitmq"
			}
			if err := ValidateSuccessfulSourceSnapshot(other, 200, []byte(b.String())); err == nil {
				t.Fatal("cross-source snapshot accepted")
			}
			if err := ValidateSuccessfulSourceSnapshot(source, 200, []byte(strings.Replace(b.String(), " 1\n", " 1\nextra_metric 1\n", 1))); err == nil {
				t.Fatal("extra metric accepted")
			}
		})
	}
}
