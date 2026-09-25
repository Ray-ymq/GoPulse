// routerpublish drives the Phase 18 Router acceptance path with persistent HTTP connections.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type report struct {
	Count         int     `json:"count"`
	Concurrency   int     `json:"concurrency"`
	Endpoints     int     `json:"endpoints"`
	Accepted      int64   `json:"accepted"`
	Failed        int64   `json:"failed"`
	ElapsedSecond float64 `json:"elapsed_seconds"`
}

func publish(endpoints []string, token string, count, concurrency, firstID int) (report, error) {
	if len(endpoints) == 0 || token == "" || count < 1 || concurrency < 1 || concurrency > count || firstID < 0 {
		return report{}, errors.New("invalid Router publish dimensions")
	}
	transport := &http.Transport{
		MaxIdleConns: concurrency * 2, MaxIdleConnsPerHost: concurrency,
		MaxConnsPerHost: concurrency, IdleConnTimeout: time.Minute,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	timestamp := "2026-01-01T00:00:00Z"
	started := time.Now()
	var accepted, failed atomic.Int64
	jobs := make(chan int, concurrency)
	var workers sync.WaitGroup
	workers.Add(concurrency)
	for worker := 0; worker < concurrency; worker++ {
		go func() {
			defer workers.Done()
			for index := range jobs {
				id := fmt.Sprintf("%032x", firstID+index)
				body := fmt.Sprintf(`{"schema_version":1,"message_id":"%s","type":"metrics","source":"redis","timestamp":"%s","payload":{"plugin_id":"redis-exporter","plugin_version":"1.11.5","target_id":"redis-exporter-local","scrape_status":"success","samples":[]}}`, id, timestamp)
				url := strings.TrimRight(endpoints[index%len(endpoints)], "/") + "/internal/v1/messages"
				request, err := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
				if err != nil {
					failed.Add(1)
					continue
				}
				request.Header.Set("Authorization", "Bearer "+token)
				request.Header.Set("Idempotency-Key", id)
				request.Header.Set("Content-Type", "application/json")
				response, err := client.Do(request)
				if err != nil {
					failed.Add(1)
					continue
				}
				_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
				_ = response.Body.Close()
				if response.StatusCode >= 200 && response.StatusCode < 300 {
					accepted.Add(1)
				} else {
					failed.Add(1)
				}
			}
		}()
	}
	for index := 0; index < count; index++ {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	result := report{
		Count: count, Concurrency: concurrency, Endpoints: len(endpoints),
		Accepted: accepted.Load(), Failed: failed.Load(), ElapsedSecond: time.Since(started).Seconds(),
	}
	if result.Failed != 0 || result.Accepted != int64(count) {
		return result, errors.New("Router rejected or lost a publish request")
	}
	return result, nil
}

func main() {
	var endpointList, reportPath string
	var count, concurrency, firstID int
	flag.StringVar(&endpointList, "endpoints", "", "comma-separated loopback Router endpoints")
	flag.StringVar(&reportPath, "report", "", "sanitized report output")
	flag.IntVar(&count, "count", 0, "number of messages")
	flag.IntVar(&concurrency, "concurrency", 64, "total publisher concurrency")
	flag.IntVar(&firstID, "first-id", 1, "first deterministic message ID")
	flag.Parse()
	if flag.NArg() != 0 || reportPath == "" {
		fmt.Fprintln(os.Stderr, "Router publisher requires report path and no positional arguments")
		os.Exit(2)
	}
	result, err := publish(strings.Split(endpointList, ","), os.Getenv("ROUTER_API_TOKEN"), count, concurrency, firstID)
	encoded, encodeErr := json.MarshalIndent(result, "", "  ")
	if encodeErr == nil {
		encodeErr = os.WriteFile(reportPath, append(encoded, '\n'), 0600)
	}
	if err != nil || encodeErr != nil {
		fmt.Fprintln(os.Stderr, "Router publication failed")
		os.Exit(1)
	}
}
