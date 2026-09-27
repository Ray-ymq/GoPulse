package componentmetrics

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	maxInstanceIDBytes  = 64
	maxReplicaEndpoints = 8
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

// InstanceID returns the bounded, non-sensitive identity used by logs and
// acceptance evidence. It is deliberately not an authorization input. The
// typed loaders reject an invalid value; this accessor keeps low-level metric
// and logging construction deterministic when a caller has not loaded config.
func InstanceID(component string) string {
	value := strings.TrimSpace(os.Getenv("GOPULSE_INSTANCE_ID"))
	if ValidateInstanceID(value) == nil {
		return value
	}
	return component + "-local"
}

// ValidateInstanceID enforces a finite, DNS-safe identity vocabulary. A
// process identity may be supplied by Compose, but it may never contain
// credentials, whitespace, or arbitrary caller data.
func ValidateInstanceID(value string) error {
	if value == "" || len(value) > maxInstanceIDBytes || strings.TrimSpace(value) != value {
		return errors.New("instance identity must be 1-64 bytes")
	}
	for index, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-' {
			if index == 0 && character == '-' {
				return errors.New("instance identity must start with a lowercase letter or digit")
			}
			continue
		}
		return errors.New("instance identity must use lowercase letters, digits, and hyphens")
	}
	if value[len(value)-1] == '-' {
		return errors.New("instance identity must not end with a hyphen")
	}
	return nil
}

// ReplicaEndpoints returns the finite list of service names that Monitor
// must scrape for a component. The list is explicit so DNS round-robin cannot
// silently make one replica disappear from evidence.
func ReplicaEndpoints(component string) ([]string, error) {
	key := strings.ToUpper(strings.ReplaceAll(component, "-", "_")) + "_ENDPOINTS"
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return []string{component}, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) == 0 || len(parts) > maxReplicaEndpoints {
		return nil, fmt.Errorf("%s must contain 1-%d endpoints", key, maxReplicaEndpoints)
	}
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		endpoint := strings.TrimSpace(part)
		if endpoint == "" || len(endpoint) > 63 || strings.ContainsAny(endpoint, " \t\r\n/:@") {
			return nil, fmt.Errorf("%s contains an invalid endpoint", key)
		}
		for _, character := range endpoint {
			if !((character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-' || character == '.') {
				return nil, fmt.Errorf("%s contains an invalid endpoint", key)
			}
		}
		if _, ok := seen[endpoint]; ok {
			return nil, fmt.Errorf("%s contains duplicate endpoints", key)
		}
		seen[endpoint] = struct{}{}
		result = append(result, endpoint)
	}
	return result, nil
}

// TargetFor gives a stable scrape target identity for an explicitly named
// replica. The historical single-instance target remains unchanged.
func TargetFor(component, endpoint string) string {
	if endpoint == "" || endpoint == component {
		return Target(component)
	}
	return endpoint + "-local"
}

func StartConfigured(ctx context.Context, id string, snapshot Snapshot) (*Listener, error) {
	return StartConfiguredWithProbes(ctx, id, snapshot, nil)
}

// StartConfiguredWithProbes serves process probes on the existing private
// listener. Metrics keep their independent authentication boundary.
func StartConfiguredWithProbes(ctx context.Context, id string, snapshot Snapshot, probes *Probes) (*Listener, error) {
	if raw := strings.TrimSpace(os.Getenv("GOPULSE_INSTANCE_ID")); raw != "" {
		if err := ValidateInstanceID(raw); err != nil {
			return nil, errors.New("invalid instance identity")
		}
	}
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
	if probes != nil {
		h = probes.Wrap(h)
	}
	listener, err := Start(ctx, Mode(), id, h)
	if err == nil {
		listener.probes = probes
	}
	return listener, err
}
