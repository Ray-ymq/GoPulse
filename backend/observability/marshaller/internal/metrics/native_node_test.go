package metrics

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/marshaller/internal/envelope"
)

func TestNativeNodeBatchImportsOriginalNamesAndMetadata(t *testing.T) {
	now := time.Now().UTC()
	const id = "0123456789abcdef0123456789abcdef"
	body := []byte(fmt.Sprintf(`{"schema_version":2,"message_id":"%s","type":"metrics","source":"node","timestamp":%q,"payload":{"dataid":1001,"time":%d,"timestamp":%d,"data":[{"dimension":{"cpu":"0","mode":"idle"},"metrics":{"node_cpu_seconds_total":42},"timestamp":%d}]}}`, id, now.Format(time.RFC3339Nano), now.Unix(), now.Unix(), now.UnixMilli()))
	message, err := (envelope.Decoder{FutureSkew: time.Minute}).Decode([]byte(id), body)
	if err != nil {
		t.Fatal(err)
	}
	result, err := (Transformer{}).Transform(message)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`node_cpu_seconds_total{source="node",target_id="node-exporter-local",producer_kind="exporter_plugin",producer_id="node-exporter",cpu="0",mode="idle"} 42 `,
		`metric="node_cpu_seconds_total",kind="counter",unit="seconds",label_keys="cpu,mode"`,
	} {
		if !strings.Contains(string(result), expected) {
			t.Fatal(string(result))
		}
	}
}
