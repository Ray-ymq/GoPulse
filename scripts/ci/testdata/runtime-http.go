// A short-lived acceptance HTTP client. It runs in an owned sidecar sharing
// only the target container's network namespace, never its writable filesystem.
package main

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) != 5 {
		os.Exit(2)
	}
	body, err := base64.StdEncoding.DecodeString(os.Args[3])
	if err != nil {
		os.Exit(2)
	}
	r, err := http.NewRequest(os.Args[1], os.Args[2], strings.NewReader(string(body)))
	if err != nil {
		os.Exit(2)
	}
	var headers map[string]string
	if json.Unmarshal([]byte(os.Args[4]), &headers) != nil {
		os.Exit(2)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(r)
	if err != nil {
		os.Exit(1)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		os.Exit(1)
	}
	output := struct {
		Status  int               `json:"status"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}{response.StatusCode, map[string]string{}, string(data)}
	for k := range response.Header {
		output.Headers[strings.ToLower(k)] = response.Header.Get(k)
	}
	_ = json.NewEncoder(os.Stdout).Encode(output)
}
