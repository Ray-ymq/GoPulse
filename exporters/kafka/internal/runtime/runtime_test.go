package runtime

import (
	"testing"
	"time"
)

func TestReadOnlyClientOptionsAreCompatible(t *testing.T) {
	// No consume topics or network requests: catch constructor option conflicts
	// before the safe --check path hides an upstream configuration error.
	c, err := Open(Config{Host: "127.0.0.1", Port: "9092", ConnectTimeout: time.Second, ScrapeTimeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	c.CloseIdleConnections()
}
