package componentmetrics

import (
	"context"
	"errors"
	"os"
	"strings"
)

func TokenKey(id string) string {
	return strings.ToUpper(strings.ReplaceAll(id, "-", "_")) + "_METRICS_TOKEN"
}
func Token(id string) (string, error) {
	token := os.Getenv(TokenKey(id))
	if len(token) < 32 || strings.ContainsAny(token, " \t\r\n") {
		return "", errors.New("invalid component metrics token")
	}
	for _, other := range Components {
		if other != id && os.Getenv(TokenKey(other)) == token {
			return "", errors.New("component metrics tokens must be independent")
		}
	}
	for _, key := range []string{"MONITOR_API_TOKEN", "ROUTER_API_TOKEN", "MARSHALLER_API_TOKEN", "MONITOR_LOG_INGEST_TOKEN", "JWT_SECRET", "AUTH_JWT_SECRET", "LOG_MONITOR_INGEST_TOKEN"} {
		if os.Getenv(key) == token {
			return "", errors.New("component metrics token must not be an API credential")
		}
	}
	return token, nil
}
func Mode() string {
	mode := os.Getenv("GOPULSE_RUNTIME_MODE")
	if mode == "" {
		return "host"
	}
	return mode
}
func StartConfigured(ctx context.Context, id string, snapshot Snapshot) (*Listener, error) {
	token, err := Token(id)
	if err != nil {
		return nil, err
	}
	spec, ok := Catalog(id)
	if !ok {
		return nil, errors.New("unknown component")
	}
	h, err := NewHandler(token, spec.MaxBodyBytes, snapshot)
	if err != nil {
		return nil, err
	}
	return Start(ctx, Mode(), id, h)
}
