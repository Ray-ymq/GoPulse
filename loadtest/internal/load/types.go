// Package load runs the Phase 18 workload and the frozen Phase 19 capacity
// profile. The legacy report types below remain available so retained Phase 18
// evidence can still be decoded without being rewritten.
package load

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

const ReportSchemaVersion = "gopulse.phase18.load.v1"

const DiagnosticSchemaVersion = "gopulse.phase18.load-diagnostic.v1"

const (
	DiagnosticProfileSchemaVersion        = "gopulse.phase20.capacity-profile.v1"
	DiagnosticCapacityReportSchemaVersion = "gopulse.phase20.load.v1"
	CapacityProfileSchemaVersion          = "gopulse.phase19.capacity-profile.v1"
	CapacityReportSchemaVersion           = "gopulse.phase19.load.v1"
	CapacityProgressSchemaVersion         = "gopulse.phase19.progress.v1"
	CapacityRepetitions                   = 3
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

type CounterSummary struct {
	Requests        uint64 `json:"requests"`
	Succeeded       uint64 `json:"succeeded"`
	ExplicitRejects uint64 `json:"explicit_rejects"`
	Timeouts        uint64 `json:"timeouts"`
	Errors          uint64 `json:"errors"`
}

func (summary *CounterSummary) Add(other CounterSummary) {
	summary.Requests += other.Requests
	summary.Succeeded += other.Succeeded
	summary.ExplicitRejects += other.ExplicitRejects
	summary.Timeouts += other.Timeouts
	summary.Errors += other.Errors
}

type LatencySummary struct {
	P50MS float64 `json:"p50_ms"`
	P95MS float64 `json:"p95_ms"`
	P99MS float64 `json:"p99_ms"`
	MaxMS float64 `json:"max_ms"`
}

type RouteReport struct {
	Category Category          `json:"category"`
	Method   string            `json:"method"`
	Template string            `json:"template"`
	Counts   CounterSummary    `json:"counts"`
	Statuses map[string]uint64 `json:"statuses"`
	Latency  LatencySummary    `json:"latency"`
}

type PhaseReport struct {
	Name              string                      `json:"name"`
	TargetRPS         float64                     `json:"target_rps"`
	DurationSeconds   float64                     `json:"duration_seconds"`
	ScheduledSlots    uint64                      `json:"scheduled_slots"`
	DroppedSlots      uint64                      `json:"dropped_slots"`
	MaxScheduleLagMS  float64                     `json:"max_schedule_lag_ms"`
	CompletedRequests uint64                      `json:"completed_requests"`
	Counts            CounterSummary              `json:"counts"`
	LatencyByCategory map[Category]LatencySummary `json:"latency_by_category"`
	CountsByCategory  map[Category]CounterSummary `json:"counts_by_category"`
}

type ProcessStats struct {
	RSSBytes       uint64 `json:"rss_bytes"`
	Goroutines     int    `json:"goroutines"`
	HeapAllocBytes uint64 `json:"heap_alloc_bytes"`
}

type Report struct {
	SchemaVersion   string                 `json:"schema_version"`
	Seed            uint64                 `json:"seed"`
	StartedAt       time.Time              `json:"started_at"`
	FinishedAt      time.Time              `json:"finished_at"`
	SteadyTargetRPS float64                `json:"steady_target_rps"`
	BurstTargetRPS  float64                `json:"burst_target_rps"`
	VirtualUsers    int                    `json:"virtual_users"`
	Phases          []PhaseReport          `json:"phases"`
	Routes          map[string]RouteReport `json:"routes"`
	Total           CounterSummary         `json:"total"`
	LoadProcess     ProcessStats           `json:"load_process"`
}

type DiagnosticRoute struct {
	Category Category          `json:"category"`
	Method   string            `json:"method"`
	Template string            `json:"template"`
	Counts   CounterSummary    `json:"counts"`
	Statuses map[string]uint64 `json:"statuses"`
	Latency  LatencySummary    `json:"latency"`
}

type DiagnosticWindow struct {
	Sequence          int                         `json:"sequence"`
	StartedAt         time.Time                   `json:"started_at"`
	Phase             string                      `json:"phase"`
	Requests          uint64                      `json:"requests"`
	Statuses          map[string]uint64           `json:"statuses"`
	Latency           LatencySummary              `json:"latency"`
	LatencyByCategory map[Category]LatencySummary `json:"latency_by_category"`
	Routes            map[string]DiagnosticRoute  `json:"routes"`
}

type ServerErrorSample struct {
	CompletedAt time.Time `json:"completed_at"`
	Phase       string    `json:"phase"`
	RequestID   string    `json:"request_id,omitempty"`
	Method      string    `json:"method"`
	Route       string    `json:"route"`
	Status      int       `json:"status"`
	ErrorCode   string    `json:"error_code,omitempty"`
	LatencyMS   float64   `json:"latency_ms"`
}

type DiagnosticReport struct {
	SchemaVersion       string              `json:"schema_version"`
	WindowSeconds       float64             `json:"window_seconds"`
	StartedAt           time.Time           `json:"started_at"`
	FinishedAt          time.Time           `json:"finished_at"`
	Windows             []DiagnosticWindow  `json:"windows"`
	ServerErrors        []ServerErrorSample `json:"server_errors"`
	ServerErrorsOmitted uint64              `json:"server_errors_omitted"`
}

// HostProfile is deliberately expressed as lower bounds. The values are part
// of the profile, rather than runner defaults, so a candidate cannot silently
// move to a smaller host.
type HostProfile struct {
	Platform          string `json:"platform"`
	HostOS            string `json:"host_os"`
	KernelContains    string `json:"kernel_contains"`
	CPUCountMin       int    `json:"cpu_count_min"`
	MemoryBytesMin    uint64 `json:"memory_bytes_min"`
	SwapBytesMin      uint64 `json:"swap_bytes_min"`
	DiskFreeBytesMin  uint64 `json:"disk_free_bytes_min"`
	DockerServerOS    string `json:"docker_server_os"`
	DockerServerArch  string `json:"docker_server_arch"`
	ComposeMinVersion string `json:"compose_min_version"`
}

type RecipeCountsProfile struct {
	Users         uint64 `json:"users"`
	Posts         uint64 `json:"posts"`
	Comments      uint64 `json:"comments"`
	PostLikes     uint64 `json:"post_likes"`
	UserFollows   uint64 `json:"user_follows"`
	PostBookmarks uint64 `json:"post_bookmarks"`
	Outbox        uint64 `json:"business_outbox"`
	Notifications uint64 `json:"notifications"`
}

type IDRangeProfile struct {
	First uint64 `json:"first"`
	Last  uint64 `json:"last"`
}

type RecipeProfile struct {
	SchemaVersion string                    `json:"schema_version"`
	Seed          uint64                    `json:"seed"`
	Counts        RecipeCountsProfile       `json:"counts"`
	IDRanges      map[string]IDRangeProfile `json:"id_ranges"`
	Digest        string                    `json:"digest"`
}

type StageProfile struct {
	Name               string  `json:"name"`
	TargetRPS          float64 `json:"target_rps"`
	WarmupTargetRPS    float64 `json:"warmup_target_rps"`
	WarmupSeconds      float64 `json:"warmup_seconds"`
	MeasurementSeconds float64 `json:"measurement_seconds"`
	RecoverySeconds    float64 `json:"recovery_seconds"`
}

type SynchronousGates struct {
	MinAchievedRPSRatio    float64 `json:"min_achieved_rps_ratio"`
	MaxP95MS               float64 `json:"max_p95_ms"`
	MaxP99MS               float64 `json:"max_p99_ms"`
	MaxTimeoutRate         float64 `json:"max_timeout_rate"`
	MaxUnexpectedErrorRate float64 `json:"max_unexpected_error_rate"`
	MaxExplicitRejectRate  float64 `json:"max_explicit_reject_rate"`
	MaxScheduleLagMS       float64 `json:"max_schedule_lag_ms"`
}

type AsynchronousGates struct {
	MaxRecoverySeconds float64 `json:"max_recovery_seconds"`
	MaxOutboxPending   uint64  `json:"max_outbox_pending"`
	MaxRabbitReady     uint64  `json:"max_rabbit_ready"`
	MaxRabbitUnacked   uint64  `json:"max_rabbit_unacked"`
	MaxKafkaLag        uint64  `json:"max_kafka_lag"`
}

type ObservabilityGates struct {
	MaxRecoverySeconds    float64 `json:"max_recovery_seconds"`
	RequireMetricProgress bool    `json:"require_metric_progress"`
	RequireLogProgress    bool    `json:"require_log_progress"`
	RequireEventProgress  bool    `json:"require_event_progress"`
}

type GateProfile struct {
	Synchronous   SynchronousGates   `json:"synchronous"`
	Asynchronous  AsynchronousGates  `json:"asynchronous"`
	Observability ObservabilityGates `json:"observability"`
}

type StopConditionProfile struct {
	OOM                      bool `json:"oom"`
	OwnershipLost            bool `json:"ownership_lost"`
	UnsafeCleanup            bool `json:"unsafe_cleanup"`
	ProfileHardError         bool `json:"profile_hard_error"`
	PreserveUnexecutedStages bool `json:"preserve_unexecuted_stages"`
}

type SamplingProfile struct {
	IntervalSeconds    float64  `json:"interval_seconds"`
	RequiredSignals    []string `json:"required_signals"`
	RequiredComponents []string `json:"required_components"`
}

type StatisticsProfile struct {
	Percentiles              []float64 `json:"percentiles"`
	Aggregations             []string  `json:"aggregations"`
	CVDefinition             string    `json:"cv_definition"`
	RetainRawRepetitions     bool      `json:"retain_raw_repetitions"`
	DoNotMergeLatencySamples bool      `json:"do_not_merge_latency_samples"`
}

type ObserverComparisonProfile struct {
	TargetRPS float64 `json:"target_rps"`
	Seconds   float64 `json:"seconds"`
	Workers   int     `json:"workers"`
	Route     string  `json:"route"`
}

type DiagnosticProfile struct {
	ObserverComparison            ObserverComparisonProfile `json:"observer_comparison"`
	DrainSeconds                  float64                   `json:"drain_seconds"`
	RecoverySeconds               float64                   `json:"recovery_seconds"`
	PollSeconds                   float64                   `json:"poll_seconds"`
	StageIsolation                string                    `json:"stage_isolation"`
	ClockAssumption               string                    `json:"clock_assumption"`
	ClockErrorMS                  float64                   `json:"clock_error_ms"`
	MetricTimestampQuantizationMS float64                   `json:"metric_timestamp_quantization_ms"`
	EventPluginID                 string                    `json:"event_plugin_id"`
	EventOperations               []string                  `json:"event_operations"`
}

type CapacityProfile struct {
	Diagnostic             *DiagnosticProfile   `json:"diagnostic,omitempty"`
	SchemaVersion          string               `json:"schema_version"`
	ProfileID              string               `json:"profile_id"`
	TargetCandidateVersion string               `json:"target_candidate_version"`
	ResourceBudget         ResourceBudgetRef    `json:"resource_budget"`
	Host                   HostProfile          `json:"host"`
	Recipe                 RecipeProfile        `json:"recipe"`
	Workload               WorkloadProfile      `json:"workload"`
	Stages                 []StageProfile       `json:"stages"`
	Repetitions            int                  `json:"repetitions"`
	Gates                  GateProfile          `json:"gates"`
	StopConditions         StopConditionProfile `json:"stop_conditions"`
	Sampling               SamplingProfile      `json:"sampling"`
	Statistics             StatisticsProfile    `json:"statistics"`
}

type ResourceBudgetRef struct {
	Path       string `json:"path"`
	Schema     string `json:"schema"`
	ContractID string `json:"contract_id"`
}

type OutcomeSummary struct {
	Requests         uint64 `json:"requests"`
	Succeeded        uint64 `json:"succeeded"`
	ExplicitRejects  uint64 `json:"explicit_rejects"`
	Rejected429      uint64 `json:"rejected_429"`
	Rejected503      uint64 `json:"rejected_503"`
	Timeouts         uint64 `json:"timeouts"`
	TransportErrors  uint64 `json:"transport_errors"`
	UnexpectedErrors uint64 `json:"unexpected_errors"`
}

func (summary *OutcomeSummary) Add(other OutcomeSummary) {
	summary.Requests += other.Requests
	summary.Succeeded += other.Succeeded
	summary.ExplicitRejects += other.ExplicitRejects
	summary.Rejected429 += other.Rejected429
	summary.Rejected503 += other.Rejected503
	summary.Timeouts += other.Timeouts
	summary.TransportErrors += other.TransportErrors
	summary.UnexpectedErrors += other.UnexpectedErrors
}

type CapacityWindowReport struct {
	Name              string            `json:"name"`
	TargetRPS         float64           `json:"target_rps"`
	DurationSeconds   float64           `json:"duration_seconds"`
	ScheduledSlots    uint64            `json:"scheduled_slots"`
	DroppedSlots      uint64            `json:"dropped_slots"`
	MaxScheduleLagMS  float64           `json:"max_schedule_lag_ms"`
	CompletedRequests uint64            `json:"completed_requests"`
	AchievedRPS       float64           `json:"achieved_rps"`
	Outcomes          OutcomeSummary    `json:"outcomes"`
	Statuses          map[string]uint64 `json:"statuses"`
	Latency           LatencySummary    `json:"latency"`
}

type CapacityStageReport struct {
	Name        string               `json:"name"`
	TargetRPS   float64              `json:"target_rps"`
	Status      string               `json:"status"`
	Warmup      CapacityWindowReport `json:"warmup"`
	Measurement CapacityWindowReport `json:"measurement"`
	Recovery    CapacityWindowReport `json:"recovery"`
}

type ProfileBinding struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
}

type CandidateBinding struct {
	Version        string `json:"version"`
	Revision       string `json:"revision"`
	ManifestSHA256 string `json:"manifest_sha256"`
}

type RepeatBinding struct {
	Number int `json:"number"`
	Total  int `json:"total"`
}

type RecipeBinding struct {
	SchemaVersion string `json:"schema_version"`
	Seed          uint64 `json:"seed"`
	Digest        string `json:"digest"`
}

type CapacityResourceReference struct {
	RawSamplesPath   string `json:"raw_samples_path"`
	SummaryPath      string `json:"summary_path"`
	RawSamplesSHA256 string `json:"raw_samples_sha256"`
	RawSampleRecords int    `json:"raw_sample_records"`
}

type CapacityProgressReference struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Records int    `json:"records"`
}

type StopInfo struct {
	Reason string `json:"reason"`
	Stage  string `json:"stage,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// CapacityProgressRecord is an append-only boundary receipt. It is written
// before and after every load/recovery window so an interrupted repetition
// retains the last trustworthy stage rather than requiring a synthesized
// summary to explain what ran.
type CapacityProgressRecord struct {
	SchemaVersion     string    `json:"schema_version"`
	Sequence          int       `json:"sequence"`
	Event             string    `json:"event"`
	Stage             string    `json:"stage,omitempty"`
	Window            string    `json:"window,omitempty"`
	Status            string    `json:"status"`
	At                time.Time `json:"at"`
	ScheduledSlots    uint64    `json:"scheduled_slots"`
	DroppedSlots      uint64    `json:"dropped_slots"`
	CompletedRequests uint64    `json:"completed_requests"`
	MaxScheduleLagMS  float64   `json:"max_schedule_lag_ms"`
}

type CapacityReport struct {
	SchemaVersion   string                     `json:"schema_version"`
	Profile         ProfileBinding             `json:"profile"`
	Candidate       CandidateBinding           `json:"candidate"`
	Recipe          RecipeBinding              `json:"recipe"`
	Repeat          RepeatBinding              `json:"repeat"`
	ExecutionStatus string                     `json:"execution_status"`
	StartedAt       time.Time                  `json:"started_at"`
	FinishedAt      time.Time                  `json:"finished_at"`
	Stages          []CapacityStageReport      `json:"stages"`
	Total           OutcomeSummary             `json:"total"`
	LoadProcess     ProcessStats               `json:"load_process"`
	Progress        *CapacityProgressReference `json:"progress,omitempty"`
	Resources       *CapacityResourceReference `json:"resources,omitempty"`
	Stop            *StopInfo                  `json:"stop,omitempty"`
}

// ProfileDigest returns a binding over the exact checked-in bytes. Keeping the
// byte-level digest avoids a second, subtly different JSON canonicalization
// implementation in the Python evidence tooling.
func ProfileDigest(encoded []byte) string {
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func LoadProfile(path string) (CapacityProfile, string, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return CapacityProfile{}, "", fmt.Errorf("read capacity profile: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	var profile CapacityProfile
	if err := decoder.Decode(&profile); err != nil {
		return CapacityProfile{}, "", fmt.Errorf("decode capacity profile: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return CapacityProfile{}, "", errors.New("capacity profile contains trailing JSON")
	}
	if err := ValidateProfile(profile); err != nil {
		return CapacityProfile{}, "", err
	}
	return profile, ProfileDigest(encoded), nil
}

func ValidateProfile(profile CapacityProfile) error {
	if (profile.SchemaVersion != CapacityProfileSchemaVersion && profile.SchemaVersion != DiagnosticProfileSchemaVersion) || !validProfileID(profile.ProfileID) || !validVersion(profile.TargetCandidateVersion) {
		return errors.New("capacity profile identity is invalid")
	}
	if profile.SchemaVersion == DiagnosticProfileSchemaVersion && (profile.ResourceBudget.Path != "deploy/phase20-resource-budgets.json" ||
		profile.ResourceBudget.Schema != "deploy/phase20-resource-budgets.schema.json" ||
		profile.ResourceBudget.ContractID != "phase20-05-budget-contract-20261001") {
		return errors.New("capacity profile resource budget binding is invalid")
	}
	if profile.SchemaVersion == DiagnosticProfileSchemaVersion {
		d := profile.Diagnostic
		if d == nil || d.ObserverComparison.TargetRPS != 50 || d.ObserverComparison.Seconds != 5 || d.ObserverComparison.Workers != 8 || d.ObserverComparison.Route != "GET /api/v1/users/me" || d.DrainSeconds != 30 || d.RecoverySeconds != 120 || d.PollSeconds != 1 || d.StageIsolation != "owned_empty_project" || d.ClockAssumption != "same_host_utc" || d.ClockErrorMS != 10 || d.MetricTimestampQuantizationMS != 1 || d.EventPluginID != "redis-exporter" || len(d.EventOperations) != 2 || d.EventOperations[0] != "stop" || d.EventOperations[1] != "start" {
			return errors.New("diagnostic recovery contract is invalid")
		}
		for _, stage := range profile.Stages {
			if stage.WarmupSeconds != 15 || stage.MeasurementSeconds != 60 {
				return errors.New("diagnostic load windows are frozen")
			}
		}
	} else if profile.Diagnostic != nil {
		return errors.New("phase19 profile cannot carry diagnostic semantics")
	}
	if profile.Host.Platform != "linux/amd64" || profile.Host.HostOS != "Linux" || profile.Host.KernelContains == "" ||
		profile.Host.CPUCountMin < 1 || profile.Host.MemoryBytesMin == 0 || profile.Host.SwapBytesMin == 0 ||
		profile.Host.DiskFreeBytesMin == 0 || profile.Host.DockerServerOS != "linux" || profile.Host.DockerServerArch != "amd64" || !validVersion(profile.Host.ComposeMinVersion) {
		return errors.New("capacity profile host contract is invalid")
	}
	if err := validateRecipeProfile(profile.Recipe); err != nil {
		return err
	}
	if err := ValidateWorkloadForSchema(profile.Workload, profile.SchemaVersion); err != nil {
		return err
	}
	if len(profile.Stages) != 4 || profile.Repetitions != CapacityRepetitions {
		return errors.New("capacity profile must contain four stages and three repetitions")
	}
	expectedRPS := []float64{50, 100, 150, 200}
	for index, stage := range profile.Stages {
		if stage.Name != fmt.Sprintf("rps-%d", int(expectedRPS[index])) || stage.TargetRPS != expectedRPS[index] ||
			stage.WarmupTargetRPS <= 0 || stage.WarmupTargetRPS >= stage.TargetRPS ||
			!boundedPositive(stage.WarmupSeconds, 3600) || !boundedPositive(stage.MeasurementSeconds, 3600) || !boundedPositive(stage.RecoverySeconds, 3600) {
			return fmt.Errorf("capacity profile stage %d is invalid", index+1)
		}
	}
	if err := validateGates(profile.Gates); err != nil {
		return err
	}
	if !profile.StopConditions.OOM || !profile.StopConditions.OwnershipLost || !profile.StopConditions.UnsafeCleanup ||
		!profile.StopConditions.ProfileHardError || !profile.StopConditions.PreserveUnexecutedStages {
		return errors.New("capacity profile must enable every safe stop condition")
	}
	if !boundedPositive(profile.Sampling.IntervalSeconds, 60) || len(profile.Sampling.RequiredSignals) == 0 || len(profile.Sampling.RequiredComponents) == 0 {
		return errors.New("capacity profile sampling contract is invalid")
	}
	expectedSignals := map[string]bool{"host_cpu": true, "host_rss": true, "load_cpu": true, "load_rss": true, "load_scheduler_lag": true, "sut_cpu": true, "sut_rss": true, "sut_saturation": true, "outbox": true, "rabbitmq": true, "kafka_lag": true}
	if len(profile.Sampling.RequiredSignals) != len(expectedSignals) {
		return errors.New("capacity profile sampling signal set is invalid")
	}
	seenSignals := make(map[string]bool, len(profile.Sampling.RequiredSignals))
	for _, signal := range profile.Sampling.RequiredSignals {
		if signal == "" || !expectedSignals[signal] || seenSignals[signal] {
			return errors.New("capacity profile contains an empty required signal")
		}
		seenSignals[signal] = true
	}
	seenComponents := make(map[string]bool, len(profile.Sampling.RequiredComponents))
	for _, component := range profile.Sampling.RequiredComponents {
		if component == "" || seenComponents[component] {
			return errors.New("capacity profile contains an empty or duplicate required component")
		}
		seenComponents[component] = true
	}
	if len(profile.Statistics.Percentiles) != 3 || profile.Statistics.Percentiles[0] != .5 || profile.Statistics.Percentiles[1] != .95 || profile.Statistics.Percentiles[2] != .99 ||
		len(profile.Statistics.Aggregations) != 4 || profile.Statistics.Aggregations[0] != "median" || profile.Statistics.Aggregations[1] != "min" || profile.Statistics.Aggregations[2] != "max" || profile.Statistics.Aggregations[3] != "cv" ||
		profile.Statistics.CVDefinition != "population_stddev_div_mean_percent" || !profile.Statistics.RetainRawRepetitions || !profile.Statistics.DoNotMergeLatencySamples {
		return errors.New("capacity profile statistics contract is invalid")
	}
	return nil
}

func validateRecipeProfile(recipe RecipeProfile) error {
	if recipe.SchemaVersion != "gopulse.phase18.recipe.v1" || recipe.Seed != 18002005 || !validDigest(recipe.Digest) || recipe.Digest != "sha256:0e61a5473f72d735ab322261e312290249f39997b649837fea32bfe2c947cf14" {
		return errors.New("capacity profile recipe identity is invalid")
	}
	expected := RecipeCountsProfile{Users: 5000, Posts: 50000, Comments: 100000, PostLikes: 200000, UserFollows: 200000, PostBookmarks: 25000, Outbox: 550000, Notifications: 500000}
	if recipe.Counts != expected {
		return errors.New("capacity profile recipe counts differ from Phase 18 contract")
	}
	expectedRanges := map[string]IDRangeProfile{"users": {First: 1, Last: 5000}, "posts": {First: 1, Last: 50000}, "comments": {First: 1, Last: 100000}}
	if len(recipe.IDRanges) != len(expectedRanges) {
		return errors.New("capacity profile recipe ID ranges are invalid")
	}
	for key, value := range expectedRanges {
		if recipe.IDRanges[key] != value {
			return errors.New("capacity profile recipe ID ranges differ from Phase 18 contract")
		}
	}
	return nil
}

func validateGates(gates GateProfile) error {
	syncGates := gates.Synchronous
	if !between(syncGates.MinAchievedRPSRatio, 0, 1) || !positiveFinite(syncGates.MaxP95MS) || !positiveFinite(syncGates.MaxP99MS) ||
		!between(syncGates.MaxTimeoutRate, 0, 1) || !between(syncGates.MaxUnexpectedErrorRate, 0, 1) || !between(syncGates.MaxExplicitRejectRate, 0, 1) || !positiveFinite(syncGates.MaxScheduleLagMS) {
		return errors.New("capacity profile synchronous gates are invalid")
	}
	asyncGates := gates.Asynchronous
	if !positiveFinite(asyncGates.MaxRecoverySeconds) || asyncGates.MaxOutboxPending < 0 || asyncGates.MaxRabbitReady < 0 || asyncGates.MaxRabbitUnacked < 0 || asyncGates.MaxKafkaLag < 0 {
		return errors.New("capacity profile asynchronous gates are invalid")
	}
	if !positiveFinite(gates.Observability.MaxRecoverySeconds) {
		return errors.New("capacity profile observability gates are invalid")
	}
	return nil
}

func positiveFinite(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func boundedPositive(value, maximum float64) bool {
	return positiveFinite(value) && value <= maximum
}
func between(value, minimum, maximum float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= minimum && value <= maximum
}

func validDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(value[len("sha256:"):])
	return err == nil
}

func validVersion(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, character := range part {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}

func validRevision(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, character := range value {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return false
		}
	}
	return true
}

func validProfileID(value string) bool {
	if len(value) < 3 || len(value) > 64 {
		return false
	}
	for index, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || (character == '-' && index > 0) {
			continue
		}
		return false
	}
	return true
}

func percentile(values []float64, fraction float64) float64 {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	index := int(fraction*float64(len(ordered)-1) + 0.999999)
	if index < 0 {
		index = 0
	}
	if index >= len(ordered) {
		index = len(ordered) - 1
	}
	return ordered[index]
}

func summarize(values []float64) LatencySummary {
	if len(values) == 0 {
		return LatencySummary{}
	}
	maximum := values[0]
	for _, value := range values[1:] {
		if value > maximum {
			maximum = value
		}
	}
	return LatencySummary{
		P50MS: percentile(values, .50), P95MS: percentile(values, .95),
		P99MS: percentile(values, .99), MaxMS: maximum,
	}
}

// DiagnosticStageReport deliberately has a distinct schema. Recovery is owned
// by the coordinator, after the drained receipt, rather than a load rest window.
type DiagnosticStageReport struct {
	SchemaVersion       string               `json:"schema_version"`
	RunID               string               `json:"run_id"`
	Stage               string               `json:"stage"`
	Repeat              RepeatBinding        `json:"repeat"`
	Profile             ProfileBinding       `json:"profile"`
	Candidate           CandidateBinding     `json:"candidate"`
	Warmup              CapacityWindowReport `json:"warmup"`
	Measurement         CapacityWindowReport `json:"measurement"`
	StopAt              time.Time            `json:"t_stop"`
	DrainAt             time.Time            `json:"t_drain"`
	DrainElapsedSeconds float64              `json:"drain_elapsed_seconds"`
	ExecutionStatus     string               `json:"execution_status"`
	Ledger              ProfileBinding       `json:"ledger"`
}

type AcceptanceRecord struct {
	LatencyMS         float64    `json:"latency_ms"`
	LoadScheduleLagMS float64    `json:"load_schedule_lag_ms"`
	RunID             string     `json:"run_id"`
	Repeat            int        `json:"repeat"`
	Stage             string     `json:"stage"`
	Window            string     `json:"window"`
	SlotID            uint64     `json:"slot_id"`
	OperationID       string     `json:"operation_id"`
	Record            string     `json:"record"`
	ActorID           uint64     `json:"actor_id"`
	Method            string     `json:"method"`
	Route             string     `json:"route_template"`
	ObjectKey         string     `json:"object_key"`
	ScheduledAt       time.Time  `json:"scheduled_at"`
	SentAt            *time.Time `json:"sent_at"`
	CompletedAt       *time.Time `json:"completed_at"`
	Status            int        `json:"status"`
	Outcome           string     `json:"outcome"`
	RequestID         string     `json:"request_id"`
	ResponseID        uint64     `json:"response_id"`
	ResponseRevision  uint64     `json:"response_revision"`
	ContentDigest     string     `json:"content_digest"`
}
