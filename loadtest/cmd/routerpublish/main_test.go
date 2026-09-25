package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestPublishUsesBothEndpointsAndUniqueIDs(t *testing.T) {
	var mutex sync.Mutex
	seen := make(map[string]bool)
	counts := [2]int{}
	servers := make([]*httptest.Server, 2)
	for index := range servers {
		index := index
		servers[index] = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodPost || request.URL.Path != "/internal/v1/messages" ||
				request.Header.Get("Authorization") != "Bearer private-token" {
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			var body struct {
				MessageID string `json:"message_id"`
			}
			if json.NewDecoder(request.Body).Decode(&body) != nil || body.MessageID != request.Header.Get("Idempotency-Key") {
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			mutex.Lock()
			defer mutex.Unlock()
			if seen[body.MessageID] {
				writer.WriteHeader(http.StatusConflict)
				return
			}
			seen[body.MessageID] = true
			counts[index]++
			writer.WriteHeader(http.StatusAccepted)
		}))
		defer servers[index].Close()
	}
	result, err := publish([]string{servers[0].URL, servers[1].URL}, "private-token", 100, 8, 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Accepted != 100 || result.Failed != 0 || counts != [2]int{50, 50} || len(seen) != 100 {
		t.Fatalf("report=%+v counts=%v unique=%d", result, counts, len(seen))
	}
}
