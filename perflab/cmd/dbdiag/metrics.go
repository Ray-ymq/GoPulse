package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

func readMetrics(ctx context.Context, client *http.Client, endpoint string) (int, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return 0, "", fmt.Errorf("invalid_metrics_response")
	}
	return response.StatusCode, string(body), nil
}
