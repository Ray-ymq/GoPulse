package envelope

import (
	"fmt"
	"testing"
	"time"
)

func TestNativeNodeBatchKeepsOriginalNames(t *testing.T) {
	now := time.Now().UTC()
	data := []byte(fmt.Sprintf(`{"schema_version":2,"message_id":"%s","type":"metrics","source":"node","timestamp":%q,"payload":{"dataid":1001,"time":%d,"timestamp":%d,"data":[{"dimension":{"cpu":"0","mode":"idle"},"metrics":{"node_cpu_seconds_total":42},"timestamp":%d}]}}`, testID, now.Format(time.RFC3339Nano), now.Unix(), now.Unix(), now.UnixMilli()))
	message, err := (Decoder{FutureSkew: time.Minute}).Decode([]byte(testID), data)
	if err != nil || message.Source != "node" || message.Payload.Data[0].Metrics["node_cpu_seconds_total"].String() != "42" {
		t.Fatal(message, err)
	}
}
