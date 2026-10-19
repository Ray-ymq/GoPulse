// Package k6 contains the small amount of Go code needed to validate and
// orchestrate the pinned k6 workload. Request generation and measurements stay
// in the k6 script so the load generator remains the process that owns them.
package k6

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	ConfigSchemaVersion = "gopulse.k6.load-config.v1"
	ReportSchemaVersion = "gopulse.k6.load.v1"
	K6Version           = "0.56.0"
)

type Mode string

const (
	ModeBoundary Mode = "boundary"
	ModeRefine   Mode = "refine"
	ModeSoak     Mode = "soak"
)

type Category string

const (
	CategoryRead         Category = "read"
	CategorySearch       Category = "search"
	CategoryNotification Category = "notification_bookmark_read"
	CategoryContentWrite Category = "content_write"
	CategoryInteraction  Category = "interaction_write"
	CategorySession      Category = "identity_session"
)

// Config is the reviewed input contract for all three load modes. Paths are
// resolved relative to the repository root when they are not absolute.
type Config struct {
	SchemaVersion  string         `json:"schema_version"`
	Target         TargetConfig   `json:"target"`
	Workload       WorkloadConfig `json:"workload"`
	Boundary       BoundaryConfig `json:"boundary"`
	Refine         RefineConfig   `json:"refine"`
	Soak           SoakConfig     `json:"soak"`
	Thresholds     Thresholds     `json:"thresholds"`
	Recovery       RecoveryConfig `json:"recovery"`
	K6             K6Config       `json:"k6"`
	ReportDir      string         `json:"report_dir"`
	PreviousReport string         `json:"previous_report,omitempty"`
}

type TargetConfig struct {
	BaseURL         string `json:"base_url"`
	HealthURL       string `json:"health_url"`
	BacklogURL      string `json:"backlog_url,omitempty"`
	BacklogPath     string `json:"backlog_json_path,omitempty"`
	CorpusPath      string `json:"corpus_path"`
	CredentialsPath string `json:"credentials_path"`
	CookieName      string `json:"cookie_name"`
}

type MixEntry struct {
	Category Category `json:"category"`
	Percent  int      `json:"percent"`
}

type WorkloadConfig struct {
	VirtualUsers          int        `json:"virtual_users"`
	RequestTimeoutSeconds float64    `json:"request_timeout_seconds"`
	Mix                   []MixEntry `json:"mix"`
}

type BoundaryConfig struct {
	StartRPS           float64 `json:"start_rps"`
	Multiplier         float64 `json:"multiplier"`
	MaxRPS             float64 `json:"max_rps"`
	WarmupSeconds      float64 `json:"warmup_seconds"`
	MeasurementSeconds float64 `json:"measurement_seconds"`
	RecoverySeconds    float64 `json:"recovery_seconds"`
}

type RefineConfig struct {
	MeasurementSeconds float64 `json:"measurement_seconds"`
	RecoverySeconds    float64 `json:"recovery_seconds"`
	MaxRelativeWidth   float64 `json:"max_relative_width"`
	MaxIterations      int     `json:"max_iterations"`
}

type SoakConfig struct {
	StableCapacityRatio float64 `json:"stable_capacity_ratio"`
	DurationSeconds     float64 `json:"duration_seconds"`
	RecoverySeconds     float64 `json:"recovery_seconds"`
}

type Thresholds struct {
	MaxP95MS                float64 `json:"max_p95_ms"`
	MaxP99MS                float64 `json:"max_p99_ms"`
	MaxErrorRate            float64 `json:"max_error_rate"`
	MinAchievedRPSRatio     float64 `json:"min_achieved_rps_ratio"`
	MaxDroppedIterationRate float64 `json:"max_dropped_iteration_rate"`
	EarlyStopErrorRate      float64 `json:"early_stop_error_rate"`
	EarlyStopSeconds        float64 `json:"early_stop_seconds"`
	HealthFailureSeconds    float64 `json:"health_failure_seconds"`
}

type RecoveryConfig struct {
	PollSeconds        float64 `json:"poll_seconds"`
	BaselineTolerance  float64 `json:"baseline_tolerance"`
	RequireObservation bool    `json:"require_observation"`
}

type K6Config struct {
	Binary string `json:"binary,omitempty"`
	Script string `json:"script"`
}

func DefaultConfig() Config {
	return Config{
		SchemaVersion: ConfigSchemaVersion,
		Target: TargetConfig{
			BaseURL:         "http://127.0.0.1:18080",
			HealthURL:       "http://127.0.0.1:18080/ready",
			CorpusPath:      ".run/load/corpus.json",
			CredentialsPath: ".run/load/credentials.json",
			CookieName:      "gopulse_session",
		},
		Workload: WorkloadConfig{
			VirtualUsers:          1024,
			RequestTimeoutSeconds: 5,
			Mix:                   defaultMix(),
		},
		Boundary:   BoundaryConfig{StartRPS: 10, Multiplier: 2, MaxRPS: 1000, WarmupSeconds: 30, MeasurementSeconds: 120, RecoverySeconds: 120},
		Refine:     RefineConfig{MeasurementSeconds: 300, RecoverySeconds: 120, MaxRelativeWidth: 0.10, MaxIterations: 8},
		Soak:       SoakConfig{StableCapacityRatio: 0.80, DurationSeconds: 1800, RecoverySeconds: 120},
		Thresholds: Thresholds{MaxP95MS: 1000, MaxP99MS: 2000, MaxErrorRate: 0.01, MinAchievedRPSRatio: 0.95, MaxDroppedIterationRate: 0.01, EarlyStopErrorRate: 0.20, EarlyStopSeconds: 30, HealthFailureSeconds: 30},
		Recovery:   RecoveryConfig{PollSeconds: 5, BaselineTolerance: 0, RequireObservation: false},
		K6:         K6Config{Script: "perflab/k6/workload.js"},
		ReportDir:  ".run/load/reports",
	}
}

func defaultMix() []MixEntry {
	return []MixEntry{
		{Category: CategoryRead, Percent: 45},
		{Category: CategorySearch, Percent: 10},
		{Category: CategoryNotification, Percent: 10},
		{Category: CategoryContentWrite, Percent: 15},
		{Category: CategoryInteraction, Percent: 15},
		{Category: CategorySession, Percent: 5},
	}
}

func LoadConfig(path string) (Config, string, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return Config{}, "", fmt.Errorf("read k6 config: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, "", fmt.Errorf("decode k6 config: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Config{}, "", errors.New("k6 config contains trailing JSON")
		}
		return Config{}, "", fmt.Errorf("decode trailing k6 config data: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return config, "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (c Config) Validate(root string) error {
	if c.SchemaVersion != ConfigSchemaVersion {
		return fmt.Errorf("unsupported k6 config schema %q", c.SchemaVersion)
	}
	if err := validateURL(c.Target.BaseURL, "target.base_url", false); err != nil {
		return err
	}
	if err := validateURL(c.Target.HealthURL, "target.health_url", true); err != nil {
		return err
	}
	if c.Target.CookieName == "" || strings.ContainsAny(c.Target.CookieName, "=;\r\n") {
		return errors.New("target.cookie_name is invalid")
	}
	if c.Target.CorpusPath == "" || c.Target.CredentialsPath == "" {
		return errors.New("target corpus and credentials paths are required")
	}
	if c.Target.BacklogURL != "" {
		if err := validateURL(c.Target.BacklogURL, "target.backlog_url", true); err != nil {
			return err
		}
	}
	if c.Workload.VirtualUsers < 1 || c.Workload.RequestTimeoutSeconds <= 0 || c.Workload.RequestTimeoutSeconds > 60 {
		return errors.New("workload virtual_users or request_timeout_seconds is invalid")
	}
	if err := validateMix(c.Workload.Mix); err != nil {
		return err
	}
	if c.Boundary.StartRPS <= 0 || c.Boundary.Multiplier <= 1 || c.Boundary.MaxRPS < c.Boundary.StartRPS || c.Boundary.WarmupSeconds <= 0 || c.Boundary.MeasurementSeconds <= 0 || c.Boundary.RecoverySeconds <= 0 {
		return errors.New("boundary parameters are invalid")
	}
	if c.Boundary.MaxRPS > 1000000 {
		return errors.New("boundary.max_rps is unreasonably large")
	}
	if c.Refine.MeasurementSeconds <= 0 || c.Refine.RecoverySeconds <= 0 || c.Refine.MaxRelativeWidth <= 0 || c.Refine.MaxRelativeWidth >= 1 || c.Refine.MaxIterations < 1 {
		return errors.New("refine parameters are invalid")
	}
	if c.Soak.StableCapacityRatio <= 0 || c.Soak.StableCapacityRatio > 1 || c.Soak.DurationSeconds <= 0 || c.Soak.RecoverySeconds <= 0 {
		return errors.New("soak parameters are invalid")
	}
	if c.Thresholds.MaxP95MS <= 0 || c.Thresholds.MaxP99MS <= 0 || c.Thresholds.MaxErrorRate < 0 || c.Thresholds.MaxErrorRate > 1 || c.Thresholds.MinAchievedRPSRatio <= 0 || c.Thresholds.MinAchievedRPSRatio > 1 || c.Thresholds.MaxDroppedIterationRate < 0 || c.Thresholds.MaxDroppedIterationRate > 1 || c.Thresholds.EarlyStopErrorRate <= 0 || c.Thresholds.EarlyStopErrorRate > 1 || c.Thresholds.EarlyStopSeconds <= 0 || c.Thresholds.HealthFailureSeconds <= 0 {
		return errors.New("threshold parameters are invalid")
	}
	if c.Recovery.PollSeconds <= 0 || c.Recovery.BaselineTolerance < 0 {
		return errors.New("recovery parameters are invalid")
	}
	if c.K6.Script == "" || c.ReportDir == "" {
		return errors.New("k6 script and report_dir are required")
	}
	if c.K6.Binary != "" && strings.ContainsAny(c.K6.Binary, "\r\n") {
		return errors.New("k6.binary is invalid")
	}
	if root == "" {
		return errors.New("repository root is required")
	}
	return nil
}

func validateMix(mix []MixEntry) error {
	if len(mix) == 0 {
		return errors.New("workload.mix is empty")
	}
	known := map[Category]bool{
		CategoryRead: true, CategorySearch: true, CategoryNotification: true,
		CategoryContentWrite: true, CategoryInteraction: true, CategorySession: true,
	}
	seen := map[Category]bool{}
	total := 0
	for _, entry := range mix {
		if !known[entry.Category] || seen[entry.Category] || entry.Percent <= 0 {
			return fmt.Errorf("workload.mix contains invalid or duplicate category %q", entry.Category)
		}
		seen[entry.Category] = true
		total += entry.Percent
	}
	if total != 100 {
		return fmt.Errorf("workload.mix totals %d%%, want 100%%", total)
	}
	return nil
}

func validateURL(value, name string, requirePath bool) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s must be an http(s) URL without credentials or query", name)
	}
	if requirePath && parsed.Path == "" {
		return fmt.Errorf("%s must include a path", name)
	}
	return nil
}

func ResolvePath(root, value string) string {
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	if _, err := os.Stat(value); err == nil {
		absolute, _ := filepath.Abs(value)
		return absolute
	}
	return filepath.Join(root, value)
}

func SortedCategories(mix []MixEntry) []string {
	values := make([]string, 0, len(mix))
	for _, entry := range mix {
		values = append(values, string(entry.Category))
	}
	sort.Strings(values)
	return values
}
