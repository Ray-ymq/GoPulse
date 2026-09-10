package collector

import (
	"fmt"
	"strings"
	"testing"
)

func TestClusterContracts(t *testing.T) {
	for _, source := range []string{"mysql", "rabbitmq"} {
		t.Run(source, func(t *testing.T) {
			var b strings.Builder
			for name, c := range contractsFor(source) {
				fmt.Fprintf(&b, "# TYPE %s %s\n", name, strings.ToLower(c.kind.String()))
				if c.count == 1 {
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
