// Package ready waits for an HTTP readiness endpoint of an owned process.
package ready

import (
	"fmt"
	"net/http"
	"time"
)

// Wait polls url until it answers 2xx, the deadline expires, or the watched
// process is gone. Proxies are ignored so a developer's proxy configuration
// cannot mask a locally owned service.
func Wait(url string, timeout time.Duration, token, label string, alive func() bool) error {
	client := &http.Client{
		Timeout:   2 * time.Second,
		Transport: &http.Transport{Proxy: nil},
	}
	deadline := time.Now().Add(timeout)
	lastError := "no response"
	for time.Now().Before(deadline) {
		if alive != nil && !alive() {
			return fmt.Errorf("%s exited before readiness; inspect its private log", label)
		}
		request, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := client.Do(request)
		if err != nil {
			lastError = err.Error()
		} else {
			response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				return nil
			}
			lastError = fmt.Sprintf("HTTP %d", response.StatusCode)
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("%s did not become ready within %ds (%s)", label, int(timeout.Seconds()), lastError)
}
