package plugin

import "time"

const PluginID = "redis-exporter"

type Manifest struct {
	SchemaVersion    int    `json:"schema_version"`
	ID               string `json:"id"`
	Name             string `json:"name"`
	Version          string `json:"version"`
	Kind             string `json:"kind"`
	Source           string `json:"source"`
	OS               string `json:"os"`
	Arch             string `json:"arch"`
	Entrypoint       string `json:"entrypoint"`
	EntrypointSHA256 string `json:"entrypoint_sha256"`
	HealthPath       string `json:"health_path"`
	MetricsPath      string `json:"metrics_path"`
}

type DesiredState string
type ObservedState string

const (
	DesiredRunning     DesiredState  = "running"
	DesiredStopped     DesiredState  = "stopped"
	ObservedInstalling ObservedState = "installing"
	ObservedStarting   ObservedState = "starting"
	ObservedRunning    ObservedState = "running"
	ObservedStopping   ObservedState = "stopping"
	ObservedStopped    ObservedState = "stopped"
	ObservedUpdating   ObservedState = "updating"
	ObservedFailed     ObservedState = "failed"
)

type SafeError struct {
	Code    string    `json:"code"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}
type Status struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Version       string        `json:"version"`
	Kind          string        `json:"kind"`
	Source        string        `json:"source"`
	DesiredState  DesiredState  `json:"desired_state"`
	ObservedState ObservedState `json:"observed_state"`
	InstalledAt   time.Time     `json:"installed_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
	StartedAt     *time.Time    `json:"started_at"`
	LastScrapeAt  *time.Time    `json:"last_scrape_at"`
	LastSuccessAt *time.Time    `json:"last_success_at"`
	LastError     *SafeError    `json:"last_error,omitempty"`
}

type registryFile struct {
	Plugins map[string]registryEntry `json:"plugins"`
}
type registryEntry struct {
	Manifest       Manifest     `json:"manifest"`
	CurrentVersion string       `json:"current_version"`
	DesiredState   DesiredState `json:"desired_state"`
	InstalledAt    time.Time    `json:"installed_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
}
type processRecord struct {
	PID               int    `json:"pid"`
	StartTicks        string `json:"start_ticks"`
	ExecutablePath    string `json:"executable_path"`
	WorkingDirectory  string `json:"working_directory"`
	CommandLineMarker string `json:"command_line_marker"`
}
