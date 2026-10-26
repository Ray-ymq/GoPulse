package envelope

import (
	"fmt"
	"testing"
	"time"
)

func TestComponentMetricsAreNoLongerAccepted(t *testing.T) {
	for _, source := range []string{"backend", "business-worker", "search-indexer", "monitor", "marshaller"} {
		body := []byte(fmt.Sprintf(`{"message_id":"0123456789abcdef0123456789abcdef","type":"metrics","source":%q,"timestamp":%q,"payload":{"data":[{"timestamp":1,"dimension":{},"metrics":{"gopulse_%s_up":1}}]}}`, source, time.Now().UTC().Format(time.RFC3339Nano), source))
		if _, err := (Decoder{}).Decode(nil, body); err == nil {
			t.Fatalf("accepted old component source %s", source)
		}
	}
}
