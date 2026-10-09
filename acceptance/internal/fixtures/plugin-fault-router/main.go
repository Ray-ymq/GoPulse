// Acceptance-only forwarding fault. Never linked into a product binary/image.
// A private bind-mounted marker selects a single metrics source; all other
// requests retain the real Router authentication, ingestion and Kafka path.
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"time"
)

func main() {
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	server := &http.Server{Addr: ":9091", ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 6 * time.Second, WriteTimeout: 8 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.RequestURI() != "/internal/v1/messages" {
			w.WriteHeader(404)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
		if err != nil || len(body) > 1<<20 {
			w.WriteHeader(400)
			return
		}
		var msg struct{ Type, Source string }
		if json.Unmarshal(body, &msg) != nil {
			w.WriteHeader(400)
			return
		}
		fault, _ := os.ReadFile("/fault/source")
		if msg.Type == "metrics" && msg.Source == string(bytes.TrimSpace(fault)) {
			w.WriteHeader(503)
			return
		}
		req, err := http.NewRequestWithContext(r.Context(), "POST", "http://router:9091/internal/v1/messages", bytes.NewReader(body))
		if err != nil {
			w.WriteHeader(503)
			return
		}
		for _, key := range []string{"Authorization", "Content-Type", "Idempotency-Key"} {
			req.Header.Set(key, r.Header.Get(key))
		}
		res, err := client.Do(req)
		if err != nil {
			w.WriteHeader(503)
			return
		}
		defer res.Body.Close()
		w.WriteHeader(res.StatusCode)
		io.Copy(w, io.LimitReader(res.Body, 4096))
	})}
	if server.ListenAndServe() != nil {
		os.Exit(1)
	}
}
