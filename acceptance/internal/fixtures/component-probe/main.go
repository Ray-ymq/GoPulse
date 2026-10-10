// Acceptance-only, owned-network probe and one fixed Backend endpoint fault.
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		target, _ := url.Parse("http://backend:19101")
		proxy := httputil.NewSingleHostReverseProxy(target)
		proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) { http.Error(w, "unavailable", 503) }
		server := &http.Server{Addr: ":19101", ReadHeaderTimeout: time.Second, WriteTimeout: 3 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, err := os.Stat("/fault/enabled"); err == nil {
				http.Error(w, "unavailable", 503)
				return
			}
			proxy.ServeHTTP(w, r)
		})}
		_ = server.ListenAndServe()
		return
	}
	var input struct {
		Component, Method, Path, Body string
		Authorization                 []string
	}
	if json.NewDecoder(io.LimitReader(os.Stdin, 4096)).Decode(&input) != nil {
		os.Exit(1)
	}
	ports := map[string]string{"backend": "19101", "business-worker": "19102", "search-indexer": "19103", "monitor": "19104", "router": "19105", "marshaller": "19106"}
	port, ok := ports[input.Component]
	if !ok {
		os.Exit(1)
	}
	request, err := http.NewRequest(input.Method, "http://"+input.Component+":"+port+input.Path, strings.NewReader(input.Body))
	if err != nil {
		os.Exit(1)
	}
	for _, a := range input.Authorization {
		request.Header.Add("Authorization", a)
	}
	client := &http.Client{Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"status": 0, "body": "unavailable"})
		return
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 262145))
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"status": response.StatusCode, "body": string(body)})
}
