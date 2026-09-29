package load

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct {
	BaseURL              string
	CookieName           string
	Corpus               Corpus
	Credentials          Credentials
	VirtualUsers         int
	RequestTimeout       time.Duration
	Warmup               time.Duration
	Steady               time.Duration
	Burst                time.Duration
	SteadyRPS            float64
	BurstRPS             float64
	ReportPath           string
	DiagnosticReportPath string
}

type requestResult struct {
	category         Category
	route            string
	method           string
	status           int
	transportFailure bool
	timeout          bool
	explicitReject   bool
	latencyMS        float64
	completedAt      time.Time
	requestID        string
	errorCode        string
}

type accumulator struct {
	counts            CounterSummary
	latency           []float64
	routes            map[string]*routeAccumulator
	byCategoryCounts  map[Category]CounterSummary
	byCategoryLatency map[Category][]float64
}

type routeAccumulator struct {
	category Category
	method   string
	template string
	counts   CounterSummary
	statuses map[string]uint64
	latency  []float64
}

type diagnosticKey struct {
	second int64
	phase  string
}

type diagnosticWindowAccumulator struct {
	startedAt time.Time
	phase     string
	values    *accumulator
	statuses  map[string]uint64
}

type diagnosticAccumulator struct {
	startedAt           time.Time
	windowSeconds       float64
	windows             map[diagnosticKey]*diagnosticWindowAccumulator
	serverErrors        []ServerErrorSample
	serverErrorLimit    int
	serverErrorsOmitted uint64
}

func newDiagnosticAccumulator(startedAt time.Time) *diagnosticAccumulator {
	return &diagnosticAccumulator{
		startedAt: startedAt, windowSeconds: 1,
		windows: make(map[diagnosticKey]*diagnosticWindowAccumulator), serverErrorLimit: 1000,
	}
}

func (value *diagnosticAccumulator) add(phase string, result requestResult) {
	completedAt := result.completedAt
	if completedAt.IsZero() {
		completedAt = time.Now()
	}
	second := int64(completedAt.Sub(value.startedAt).Seconds())
	if second < 0 {
		second = 0
	}
	key := diagnosticKey{second: second, phase: phase}
	window := value.windows[key]
	if window == nil {
		window = &diagnosticWindowAccumulator{
			startedAt: value.startedAt.Add(time.Duration(second) * time.Second),
			phase:     phase, values: newAccumulator(), statuses: make(map[string]uint64),
		}
		value.windows[key] = window
	}
	window.values.add(result)
	window.statuses[diagnosticStatus(result)]++
	if result.status >= http.StatusInternalServerError {
		if len(value.serverErrors) < value.serverErrorLimit {
			value.serverErrors = append(value.serverErrors, ServerErrorSample{
				CompletedAt: completedAt.UTC(), Phase: phase, Method: result.method, Route: result.route,
				Status: result.status, ErrorCode: safeErrorCode(result.errorCode), LatencyMS: result.latencyMS,
				RequestID: safeRequestID(result.requestID),
			})
		} else {
			value.serverErrorsOmitted++
		}
	}
}

func (value *diagnosticAccumulator) report(finishedAt time.Time) DiagnosticReport {
	keys := make([]diagnosticKey, 0, len(value.windows))
	for key := range value.windows {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].second != keys[right].second {
			return keys[left].second < keys[right].second
		}
		return keys[left].phase < keys[right].phase
	})
	windows := make([]DiagnosticWindow, 0, len(keys))
	for _, key := range keys {
		window := value.windows[key]
		routes := make(map[string]DiagnosticRoute, len(window.values.routes))
		for name, route := range window.values.routes {
			routes[name] = DiagnosticRoute{
				Category: route.category, Method: route.method, Template: route.template,
				Counts: route.counts, Statuses: route.statuses, Latency: summarize(route.latency),
			}
		}
		windows = append(windows, DiagnosticWindow{
			Sequence: int(key.second), StartedAt: window.startedAt.UTC(), Phase: window.phase,
			Requests: window.values.counts.Requests, Statuses: window.statuses,
			Latency:           summarize(window.values.latency),
			LatencyByCategory: summarizeCategories(window.values.byCategoryLatency), Routes: routes,
		})
	}
	serverErrors := value.serverErrors
	if serverErrors == nil {
		serverErrors = make([]ServerErrorSample, 0)
	}
	return DiagnosticReport{
		SchemaVersion: DiagnosticSchemaVersion, WindowSeconds: value.windowSeconds,
		StartedAt: value.startedAt.UTC(), FinishedAt: finishedAt.UTC(), Windows: windows,
		ServerErrors: serverErrors, ServerErrorsOmitted: value.serverErrorsOmitted,
	}
}

func diagnosticStatus(result requestResult) string {
	if result.timeout {
		return "timeout"
	}
	if result.transportFailure {
		return "transport_error"
	}
	return strconv.Itoa(result.status)
}

func safeRequestID(value string) string {
	if len(value) != 32 {
		return ""
	}
	for _, character := range value {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return ""
		}
	}
	return value
}

func safeErrorCode(value string) string {
	if len(value) < 1 || len(value) > 64 {
		return ""
	}
	for _, character := range value {
		if !((character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '_') {
			return ""
		}
	}
	return value
}

func newAccumulator() *accumulator {
	return &accumulator{
		routes:            make(map[string]*routeAccumulator),
		byCategoryCounts:  make(map[Category]CounterSummary),
		byCategoryLatency: make(map[Category][]float64),
	}
}

func (value *accumulator) add(result requestResult) {
	value.counts.Requests++
	value.latency = append(value.latency, result.latencyMS)
	categoryCounts := value.byCategoryCounts[result.category]
	categoryCounts.Requests++
	if result.timeout {
		value.counts.Timeouts++
		categoryCounts.Timeouts++
	} else if result.transportFailure {
		value.counts.Errors++
		categoryCounts.Errors++
	} else if result.explicitReject {
		value.counts.ExplicitRejects++
		categoryCounts.ExplicitRejects++
	} else if result.status >= 200 && result.status < 300 {
		value.counts.Succeeded++
		categoryCounts.Succeeded++
	} else {
		value.counts.Errors++
		categoryCounts.Errors++
	}
	value.byCategoryCounts[result.category] = categoryCounts
	value.byCategoryLatency[result.category] = append(value.byCategoryLatency[result.category], result.latencyMS)
	route := value.routes[result.route]
	if route == nil {
		route = &routeAccumulator{category: result.category, method: result.method, template: result.route, statuses: make(map[string]uint64)}
		value.routes[result.route] = route
	}
	route.counts = categoryCountsForRoute(route.counts, result)
	status := strconv.Itoa(result.status)
	if result.timeout {
		status = "timeout"
	} else if result.transportFailure {
		status = "transport_error"
	}
	route.statuses[status]++
	route.latency = append(route.latency, result.latencyMS)
}

func categoryCountsForRoute(summary CounterSummary, result requestResult) CounterSummary {
	summary.Requests++
	if result.timeout {
		summary.Timeouts++
	} else if result.transportFailure {
		summary.Errors++
	} else if result.explicitReject {
		summary.ExplicitRejects++
	} else if result.status >= 200 && result.status < 300 {
		summary.Succeeded++
	} else {
		summary.Errors++
	}
	return summary
}

func Run(ctx context.Context, config Config) (Report, error) {
	if config.VirtualUsers < 1 || config.SteadyRPS <= 0 || config.BurstRPS <= 0 || config.RequestTimeout <= 0 {
		return Report{}, errors.New("load configuration is invalid")
	}
	if config.CookieName == "" {
		config.CookieName = "gopulse_session"
	}
	if len(config.Corpus.Users) < config.VirtualUsers || len(config.Credentials.Users) < config.VirtualUsers {
		return Report{}, errors.New("load corpus has fewer users than virtual users")
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:          config.VirtualUsers * 2,
		MaxIdleConnsPerHost:   config.VirtualUsers,
		MaxConnsPerHost:       0,
		IdleConnTimeout:       5 * time.Minute,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	cookies, err := prepareSessions(ctx, client, config)
	if err != nil {
		return Report{}, err
	}

	started := time.Now().UTC()
	diagnostics := newDiagnosticAccumulator(started)
	jobs := make([]chan scheduledSlot, config.VirtualUsers)
	for id := range jobs {
		jobs[id] = make(chan scheduledSlot, 4)
	}
	accumulators := map[string]*accumulator{
		"warmup": newAccumulator(),
		"steady": newAccumulator(),
		"burst":  newAccumulator(),
	}
	var workerError atomic.Value
	var workers sync.WaitGroup
	var aggregateMu sync.Mutex
	globalRoutes := newAccumulator()
	workers.Add(config.VirtualUsers)
	for id := 0; id < config.VirtualUsers; id++ {
		state := &vuState{id: id, corpus: &config.Corpus, credentials: &config.Credentials}
		go func(id int, state *vuState, cookie string) {
			defer workers.Done()
			for slot := range jobs[id] {
				if workerError.Load() != nil {
					continue
				}
				request := state.request(slot.index)
				result := executeRequest(ctx, client, config.BaseURL, config.CookieName, cookie, request, slot.scheduledAt, config.RequestTimeout)
				aggregateMu.Lock()
				accumulators[slot.phase].add(result)
				globalRoutes.add(result)
				diagnostics.add(slot.phase, result)
				aggregateMu.Unlock()
			}
		}(id, state, cookies[id])
	}

	phases := []phaseSpec{
		{name: "warmup", duration: config.Warmup, rps: config.SteadyRPS, ramp: true},
		{name: "steady", duration: config.Steady, rps: config.SteadyRPS},
		{name: "burst", duration: config.Burst, rps: config.BurstRPS},
	}
	phaseReports := make([]PhaseReport, 0, len(phases))
	slotIndex := uint64(0)
	for _, phase := range phases {
		if phase.duration <= 0 {
			continue
		}
		scheduled, dropped, maxLag, err := schedulePhase(ctx, phase, jobs, &slotIndex)
		if err != nil {
			workerError.Store(err)
			closeJobs(jobs)
			workers.Wait()
			return Report{}, err
		}
		phaseReports = append(phaseReports, PhaseReport{
			Name: phase.name, TargetRPS: phase.rps, DurationSeconds: phase.duration.Seconds(),
			ScheduledSlots: scheduled, DroppedSlots: dropped, MaxScheduleLagMS: maxLag,
		})
	}
	closeJobs(jobs)
	workers.Wait()
	if value := workerError.Load(); value != nil {
		if err, ok := value.(error); ok && err != nil {
			return Report{}, err
		}
	}
	finished := time.Now().UTC()
	for index := range phaseReports {
		value := accumulators[phaseReports[index].Name]
		phaseReports[index].Counts = value.counts
		phaseReports[index].CompletedRequests = value.counts.Requests
		phaseReports[index].LatencyByCategory = summarizeCategories(value.byCategoryLatency)
		phaseReports[index].CountsByCategory = value.byCategoryCounts
	}
	report := Report{
		SchemaVersion: ReportSchemaVersion, Seed: config.Corpus.Seed,
		StartedAt: started, FinishedAt: finished,
		SteadyTargetRPS: config.SteadyRPS, BurstTargetRPS: config.BurstRPS,
		VirtualUsers: config.VirtualUsers, Phases: phaseReports,
		Routes: renderRoutes(globalRoutes.routes), Total: globalRoutes.counts,
		LoadProcess: processStats(),
	}
	if config.ReportPath != "" {
		if err := writeReportAtomic(config.ReportPath, report); err != nil {
			return Report{}, err
		}
	}
	if config.DiagnosticReportPath != "" {
		if err := writeDiagnosticAtomic(config.DiagnosticReportPath, diagnostics.report(finished)); err != nil {
			return Report{}, err
		}
	}
	return report, nil
}

// CapacityRunConfig contains only runtime inputs. Rate, window, repetition,
// timeout, and gate values are read from the validated profile; none of them
// can be overridden by a caller of the formal load command.
type CapacityRunConfig struct {
	BaseURL       string
	CookieName    string
	Corpus        Corpus
	Credentials   Credentials
	Profile       CapacityProfile
	ProfileDigest string
	Candidate     CandidateBinding
	Repeat        int
	ReportPath    string
	ProgressPath  string
	Resources     *CapacityResourceReference
}

type capacityProgressWriter struct {
	file     *os.File
	sequence int
}

func newCapacityProgressWriter(path string) (*capacityProgressWriter, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open capacity progress: %w", err)
	}
	return &capacityProgressWriter{file: file}, nil
}

func (writer *capacityProgressWriter) write(event, stage, window, status string, scheduled, dropped, completed uint64, lag float64) error {
	if writer == nil {
		return nil
	}
	record := CapacityProgressRecord{
		SchemaVersion: CapacityProgressSchemaVersion, Sequence: writer.sequence,
		Event: event, Stage: stage, Window: window, Status: status, At: time.Now().UTC(),
		ScheduledSlots: scheduled, DroppedSlots: dropped, CompletedRequests: completed,
		MaxScheduleLagMS: lag,
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if _, err := writer.file.Write(encoded); err != nil {
		return err
	}
	if err := writer.file.Sync(); err != nil {
		return err
	}
	writer.sequence++
	return nil
}

func (writer *capacityProgressWriter) close() error {
	if writer == nil || writer.file == nil {
		return nil
	}
	return writer.file.Close()
}

type capacitySlot struct {
	index       uint64
	stage       int
	window      string
	scheduledAt time.Time
}

type capacityAccumulator struct {
	targetRPS        float64
	duration         time.Duration
	scheduled        uint64
	dropped          uint64
	maxScheduleLagMS float64
	outcomes         OutcomeSummary
	statuses         map[string]uint64
	latency          []float64
}

func newCapacityAccumulator(targetRPS float64, duration time.Duration) *capacityAccumulator {
	return &capacityAccumulator{targetRPS: targetRPS, duration: duration, statuses: make(map[string]uint64)}
}

func (value *capacityAccumulator) add(result requestResult) {
	value.outcomes.Requests++
	value.latency = append(value.latency, result.latencyMS)
	status := strconv.Itoa(result.status)
	if result.timeout {
		value.outcomes.Timeouts++
		status = "timeout"
	} else if result.transportFailure {
		value.outcomes.TransportErrors++
		status = "transport_error"
	} else if result.explicitReject {
		value.outcomes.ExplicitRejects++
		if result.status == http.StatusTooManyRequests {
			value.outcomes.Rejected429++
		}
		if result.status == http.StatusServiceUnavailable {
			value.outcomes.Rejected503++
		}
	} else if result.status >= 200 && result.status < 300 {
		value.outcomes.Succeeded++
	} else {
		value.outcomes.UnexpectedErrors++
	}
	value.statuses[status]++
}

func (value *capacityAccumulator) report(name string) CapacityWindowReport {
	duration := value.duration.Seconds()
	achieved := float64(value.outcomes.Requests) / duration
	if duration <= 0 {
		achieved = 0
	}
	return CapacityWindowReport{
		Name: name, TargetRPS: value.targetRPS, DurationSeconds: duration,
		ScheduledSlots: value.scheduled, DroppedSlots: value.dropped,
		MaxScheduleLagMS: value.maxScheduleLagMS, CompletedRequests: value.outcomes.Requests,
		AchievedRPS: achieved, Outcomes: value.outcomes,
		Statuses: value.statuses, Latency: summarize(value.latency),
	}
}

// RunCapacity executes exactly one independent repetition. The Python
// orchestrator owns the three-repetition evidence directory and calls this
// function once per repetition, so a partial run can preserve every stage that
// was not reached without fabricating values.
func RunCapacity(ctx context.Context, config CapacityRunConfig) (CapacityReport, error) {
	if err := ValidateProfile(config.Profile); err != nil {
		return CapacityReport{}, err
	}
	if config.BaseURL == "" || !validDigest(config.ProfileDigest) || config.Repeat < 1 || config.Repeat > config.Profile.Repetitions ||
		config.Candidate.Version != config.Profile.TargetCandidateVersion || !validVersion(config.Candidate.Version) || !validDigest(config.Candidate.ManifestSHA256) || !validRevision(config.Candidate.Revision) || config.ReportPath == "" {
		return CapacityReport{}, errors.New("capacity run binding is invalid")
	}
	if len(config.Corpus.Users) < config.Profile.Workload.VirtualUsers || len(config.Credentials.Users) < config.Profile.Workload.VirtualUsers ||
		config.Corpus.Seed != config.Profile.Recipe.Seed {
		return CapacityReport{}, errors.New("capacity artifacts do not match the profile")
	}
	progress, err := newCapacityProgressWriter(config.ProgressPath)
	if err != nil {
		return CapacityReport{}, err
	}
	completed := false
	defer func() {
		if progress == nil {
			return
		}
		status := "incomplete"
		if completed {
			status = "complete"
		}
		_ = progress.write("run_finished", "", "", status, 0, 0, 0, 0)
		_ = progress.close()
	}()
	if err := progress.write("run_started", "", "", "started", 0, 0, 0, 0); err != nil {
		return CapacityReport{}, err
	}
	if config.CookieName == "" {
		config.CookieName = "gopulse_session"
	}
	requestTimeout := time.Duration(config.Profile.Workload.RequestTimeoutSeconds * float64(time.Second))
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:          config.Profile.Workload.VirtualUsers * 2,
		MaxIdleConnsPerHost:   config.Profile.Workload.VirtualUsers,
		IdleConnTimeout:       5 * time.Minute,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	cookies, err := prepareSessions(ctx, client, Config{
		BaseURL: config.BaseURL, CookieName: config.CookieName, Corpus: config.Corpus,
		Credentials: config.Credentials, VirtualUsers: config.Profile.Workload.VirtualUsers,
		RequestTimeout: requestTimeout,
	})
	if err != nil {
		return CapacityReport{}, err
	}

	started := time.Now().UTC()
	jobs := make([]chan capacitySlot, config.Profile.Workload.VirtualUsers)
	for index := range jobs {
		jobs[index] = make(chan capacitySlot, 16)
	}
	accumulators := make(map[string]*capacityAccumulator)
	for stageIndex, stage := range config.Profile.Stages {
		accumulators[capacityWindowKey(stageIndex, "warmup")] = newCapacityAccumulator(stage.WarmupTargetRPS, secondsDuration(stage.WarmupSeconds))
		accumulators[capacityWindowKey(stageIndex, "measurement")] = newCapacityAccumulator(stage.TargetRPS, secondsDuration(stage.MeasurementSeconds))
		accumulators[capacityWindowKey(stageIndex, "recovery")] = newCapacityAccumulator(0, secondsDuration(stage.RecoverySeconds))
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var workers sync.WaitGroup
	var workerError atomic.Value
	var aggregateMu sync.Mutex
	workers.Add(len(jobs))
	for id := range jobs {
		state := &vuState{id: id, corpus: &config.Corpus, credentials: &config.Credentials, profile: &config.Profile.Workload}
		go func(id int, state *vuState, cookie string) {
			defer workers.Done()
			for slot := range jobs[id] {
				if workerError.Load() != nil {
					continue
				}
				request := state.request(slot.index)
				if err := ValidateRequest(config.Profile.Workload, request); err != nil {
					workerError.Store(err)
					cancel()
					continue
				}
				result := executeRequest(runCtx, client, config.BaseURL, config.CookieName, cookie, request, slot.scheduledAt, requestTimeout)
				aggregateMu.Lock()
				accumulators[capacityWindowKey(slot.stage, slot.window)].add(result)
				aggregateMu.Unlock()
			}
		}(id, state, cookies[id])
	}

	var slotIndex uint64
	stopScheduling := func() {
		for _, channel := range jobs {
			close(channel)
		}
		workers.Wait()
	}
	for stageIndex, stage := range config.Profile.Stages {
		windows := []struct {
			name     string
			target   float64
			duration float64
		}{
			{name: "warmup", target: stage.WarmupTargetRPS, duration: stage.WarmupSeconds},
			{name: "measurement", target: stage.TargetRPS, duration: stage.MeasurementSeconds},
		}
		for _, window := range windows {
			if err := progress.write("window_started", stage.Name, window.name, "started", 0, 0, 0, 0); err != nil {
				stopScheduling()
				return CapacityReport{}, err
			}
			scheduled, dropped, maxLag, scheduleErr := scheduleCapacityWindow(runCtx, window.target, window.duration, jobs, &slotIndex, stageIndex, window.name)
			value := accumulators[capacityWindowKey(stageIndex, window.name)]
			value.scheduled, value.dropped, value.maxScheduleLagMS = scheduled, dropped, maxLag
			if scheduleErr != nil {
				stopScheduling()
				return CapacityReport{}, scheduleErr
			}
			if err := progress.write("window_finished", stage.Name, window.name, "complete", scheduled, dropped, 0, maxLag); err != nil {
				stopScheduling()
				return CapacityReport{}, err
			}
		}
		if err := progress.write("recovery_started", stage.Name, "recovery", "started", 0, 0, 0, 0); err != nil {
			stopScheduling()
			return CapacityReport{}, err
		}
		recovery := time.NewTimer(secondsDuration(stage.RecoverySeconds))
		select {
		case <-runCtx.Done():
			if !recovery.Stop() {
				<-recovery.C
			}
			stopScheduling()
			return CapacityReport{}, runCtx.Err()
		case <-recovery.C:
		}
		if err := progress.write("recovery_finished", stage.Name, "recovery", "complete", 0, 0, 0, 0); err != nil {
			stopScheduling()
			return CapacityReport{}, err
		}
	}
	stopScheduling()
	if value := workerError.Load(); value != nil {
		if err, ok := value.(error); ok {
			return CapacityReport{}, fmt.Errorf("capacity workload contract failed: %w", err)
		}
	}

	finished := time.Now().UTC()
	stages := make([]CapacityStageReport, 0, len(config.Profile.Stages))
	total := OutcomeSummary{}
	for stageIndex, stage := range config.Profile.Stages {
		warmup := accumulators[capacityWindowKey(stageIndex, "warmup")].report("warmup")
		measurement := accumulators[capacityWindowKey(stageIndex, "measurement")].report("measurement")
		recovery := accumulators[capacityWindowKey(stageIndex, "recovery")].report("recovery")
		total.Add(warmup.Outcomes)
		total.Add(measurement.Outcomes)
		stages = append(stages, CapacityStageReport{
			Name: stage.Name, TargetRPS: stage.TargetRPS, Status: "complete",
			Warmup: warmup, Measurement: measurement, Recovery: recovery,
		})
	}
	report := CapacityReport{
		SchemaVersion:   CapacityReportSchemaVersion,
		Profile:         ProfileBinding{ID: config.Profile.ProfileID, SHA256: config.ProfileDigest},
		Candidate:       config.Candidate,
		Recipe:          RecipeBinding{SchemaVersion: config.Profile.Recipe.SchemaVersion, Seed: config.Profile.Recipe.Seed, Digest: config.Profile.Recipe.Digest},
		Repeat:          RepeatBinding{Number: config.Repeat, Total: config.Profile.Repetitions},
		ExecutionStatus: "complete", StartedAt: started, FinishedAt: finished,
		Stages: stages, Total: total, LoadProcess: processStats(), Resources: config.Resources,
	}
	if progress != nil {
		if err := progress.write("run_finished", "", "", "complete", 0, 0, 0, 0); err != nil {
			return CapacityReport{}, err
		}
		completed = true
		if err := progress.close(); err != nil {
			return CapacityReport{}, err
		}
		progressReference, err := readCapacityProgressReference(config.ProgressPath)
		if err != nil {
			return CapacityReport{}, err
		}
		report.Progress = progressReference
		progress = nil
	}
	if err := writeCapacityReportAtomic(config.ReportPath, report); err != nil {
		return CapacityReport{}, err
	}
	completed = true
	return report, nil
}

func readCapacityProgressReference(path string) (*CapacityProgressReference, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read capacity progress: %w", err)
	}
	records := 0
	for _, line := range strings.Split(strings.TrimSpace(string(encoded)), "\n") {
		if strings.TrimSpace(line) != "" {
			records++
		}
	}
	if records == 0 {
		return nil, errors.New("capacity progress is empty")
	}
	return &CapacityProgressReference{Path: "progress.jsonl", SHA256: ProfileDigest(encoded), Records: records}, nil
}

func secondsDuration(seconds float64) time.Duration {
	return time.Duration(seconds * float64(time.Second))
}

func capacityWindowKey(stage int, window string) string { return fmt.Sprintf("%d:%s", stage, window) }

func scheduleCapacityWindow(ctx context.Context, targetRPS, seconds float64, jobs []chan capacitySlot, slotIndex *uint64, stage int, window string) (scheduled, dropped uint64, maxLagMS float64, resultErr error) {
	if targetRPS <= 0 || seconds <= 0 || len(jobs) == 0 {
		return 0, 0, 0, errors.New("capacity window is invalid")
	}
	count := uint64(math.Round(targetRPS * seconds))
	started := time.Now()
	timer := time.NewTimer(0)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	for index := uint64(0); index < count; index++ {
		if err := ctx.Err(); err != nil {
			return 0, dropped, maxLagMS, err
		}
		scheduledAt := started.Add(fixedScheduleOffset(index, targetRPS))
		if wait := time.Until(scheduledAt); wait > 0 {
			timer.Reset(wait)
			select {
			case <-ctx.Done():
				return 0, dropped, maxLagMS, ctx.Err()
			case <-timer.C:
			}
		}
		if lag := time.Since(scheduledAt); lag > 0 {
			lagMS := float64(lag) / float64(time.Millisecond)
			if lagMS > maxLagMS {
				maxLagMS = lagMS
			}
		}
		virtualUser := int(*slotIndex % uint64(len(jobs)))
		slot := capacitySlot{index: *slotIndex, stage: stage, window: window, scheduledAt: scheduledAt}
		select {
		case jobs[virtualUser] <- slot:
			scheduled++
		default:
			dropped++
		}
		*slotIndex++
	}
	return scheduled, dropped, maxLagMS, nil
}

func writeCapacityReportAtomic(path string, report CapacityReport) error {
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return errors.New("encode capacity report")
	}
	encoded = append(encoded, '\n')
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, encoded, 0o600); err != nil {
		return fmt.Errorf("write capacity report: %w", err)
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("protect capacity report: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("publish capacity report: %w", err)
	}
	return nil
}

type scheduledSlot struct {
	index       uint64
	phase       string
	scheduledAt time.Time
}

func closeJobs(jobs []chan scheduledSlot) {
	for _, channel := range jobs {
		close(channel)
	}
}

type phaseSpec struct {
	name     string
	duration time.Duration
	rps      float64
	ramp     bool
}

func schedulePhase(ctx context.Context, phase phaseSpec, jobs []chan scheduledSlot, slotIndex *uint64) (scheduled, dropped uint64, maxLagMS float64, resultErr error) {
	count := uint64(math.Round(phase.duration.Seconds() * phase.rps))
	if phase.ramp {
		count = uint64(math.Round(phase.duration.Seconds() * phase.rps / 2))
	}
	started := time.Now()
	timer := time.NewTimer(0)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	for index := uint64(0); index < count; index++ {
		if err := ctx.Err(); err != nil {
			return 0, dropped, maxLagMS, err
		}
		offset := fixedScheduleOffset(index, phase.rps)
		if phase.ramp {
			offset = rampScheduleOffset(index, phase.duration, phase.rps)
		}
		scheduledAt := started.Add(offset)
		wait := time.Until(scheduledAt)
		if wait > 0 {
			timer.Reset(wait)
			select {
			case <-ctx.Done():
				return 0, dropped, maxLagMS, ctx.Err()
			case <-timer.C:
			}
		}
		lag := time.Since(scheduledAt)
		if lag > 0 {
			value := float64(lag) / float64(time.Millisecond)
			if value > maxLagMS {
				maxLagMS = value
			}
		}
		virtualUser := int(*slotIndex % uint64(len(jobs)))
		select {
		case jobs[virtualUser] <- scheduledSlot{index: *slotIndex, phase: phase.name, scheduledAt: scheduledAt}:
		default:
			dropped++
		}
		*slotIndex++
	}
	return count, dropped, maxLagMS, nil
}

func fixedScheduleOffset(index uint64, rps float64) time.Duration {
	return time.Duration(float64(index) * float64(time.Second) / rps)
}

func rampScheduleOffset(index uint64, duration time.Duration, targetRPS float64) time.Duration {
	seconds := duration.Seconds()
	if index == 0 {
		return 0
	}
	offsetSeconds := math.Sqrt(2 * float64(index) * seconds / targetRPS)
	return time.Duration(offsetSeconds * float64(time.Second))
}

func executeRequest(ctx context.Context, client *http.Client, baseURL, cookieName, cookie string, request Request, scheduledAt time.Time, timeout time.Duration) requestResult {
	result := requestResult{category: request.Category, route: request.Template, method: request.Method}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var body io.Reader
	if len(request.Body) > 0 {
		body = strings.NewReader(string(request.Body))
	}
	httpRequest, err := http.NewRequestWithContext(requestCtx, request.Method, strings.TrimRight(baseURL, "/")+request.Path, body)
	if err != nil {
		result.transportFailure = true
		result.latencyMS = float64(time.Since(scheduledAt)) / float64(time.Millisecond)
		result.completedAt = time.Now()
		return result
	}
	httpRequest.Header.Set("Cookie", cookieName+"="+cookie)
	if len(request.Body) > 0 {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(httpRequest)
	result.latencyMS = float64(time.Since(scheduledAt)) / float64(time.Millisecond)
	result.completedAt = time.Now()
	if err != nil {
		result.transportFailure = true
		var networkError net.Error
		result.timeout = errors.Is(err, context.DeadlineExceeded) || errors.As(err, &networkError) && networkError.Timeout()
		return result
	}
	defer response.Body.Close()
	bodyBytes, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	_, _ = io.Copy(io.Discard, response.Body)
	result.status = response.StatusCode
	result.requestID = response.Header.Get("X-Request-ID")
	result.errorCode = responseErrorCode(bodyBytes)
	if response.StatusCode >= 200 && response.StatusCode < 300 && request.ExpectedStatuses[response.StatusCode] {
		return result
	}
	if isExplicitReject(response.StatusCode, response.Header.Get("Retry-After"), bodyBytes) {
		result.explicitReject = true
		return result
	}
	return result
}

func isExplicitReject(status int, retryAfter string, body []byte) bool {
	if status != http.StatusTooManyRequests && status != http.StatusServiceUnavailable {
		return false
	}
	code := responseErrorCode(body)
	if code == "backend_busy" || code == "server_overloaded" {
		return true
	}
	return strings.TrimSpace(retryAfter) != "" && code == "rate_limited"
}

func bytesContainJSONCode(body []byte, code string) bool {
	return responseErrorCode(body) == code
}

func responseErrorCode(body []byte) string {
	var document struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &document) != nil {
		return ""
	}
	return document.Error.Code
}

func prepareSessions(ctx context.Context, client *http.Client, config Config) ([]string, error) {
	cookies := make([]string, config.VirtualUsers)
	errorsChannel := make(chan error, config.VirtualUsers)
	jobs := make(chan int)
	var workers sync.WaitGroup
	workerCount := 32
	if config.VirtualUsers < workerCount {
		workerCount = config.VirtualUsers
	}
	workers.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer workers.Done()
			for index := range jobs {
				cookie, err := loginSession(ctx, client, config.BaseURL, config.CookieName, config.Credentials.Users[index], config.Credentials.Password, config.RequestTimeout)
				if err != nil {
					errorsChannel <- err
					continue
				}
				cookies[index] = cookie
			}
		}()
	}
	for index := 0; index < config.VirtualUsers; index++ {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			return nil, fmt.Errorf("prepare load sessions: %w", err)
		}
	}
	return cookies, nil
}

func loginSession(ctx context.Context, client *http.Client, baseURL, cookieName string, user User, password string, timeout time.Duration) (string, error) {
	request := LoginRequest(user, password)
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(requestCtx, request.Method, strings.TrimRight(baseURL, "/")+request.Path, strings.NewReader(string(request.Body)))
	if err != nil {
		return "", err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := client.Do(httpRequest)
	if err != nil {
		return "", errors.New("login request failed")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login status %d", response.StatusCode)
	}
	for _, cookie := range response.Cookies() {
		if cookie.Name == cookieName && cookie.Value != "" {
			return cookie.Value, nil
		}
	}
	return "", errors.New("login response omitted session cookie")
}

func renderRoutes(values map[string]*routeAccumulator) map[string]RouteReport {
	result := make(map[string]RouteReport, len(values))
	for key, value := range values {
		result[key] = RouteReport{
			Category: value.category, Method: value.method, Template: value.template,
			Counts: value.counts, Statuses: value.statuses, Latency: summarize(value.latency),
		}
	}
	return result
}

func summarizeCategories(values map[Category][]float64) map[Category]LatencySummary {
	result := make(map[Category]LatencySummary, len(values))
	for category, samples := range values {
		result[category] = summarize(samples)
	}
	return result
}

func processStats() ProcessStats {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	return ProcessStats{RSSBytes: residentSetBytes(), Goroutines: runtime.NumGoroutine(), HeapAllocBytes: memory.HeapAlloc}
}

func residentSetBytes() uint64 {
	encoded, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(encoded))
	if len(fields) < 2 {
		return 0
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return pages * uint64(os.Getpagesize())
}

func writeReportAtomic(path string, report Report) error {
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return errors.New("encode load report")
	}
	encoded = append(encoded, '\n')
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, encoded, 0o600); err != nil {
		return fmt.Errorf("write load report: %w", err)
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("protect load report: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("publish load report: %w", err)
	}
	return nil
}

func writeDiagnosticAtomic(path string, report DiagnosticReport) error {
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return errors.New("encode load diagnostic report")
	}
	encoded = append(encoded, '\n')
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, encoded, 0o600); err != nil {
		return fmt.Errorf("write load diagnostic report: %w", err)
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("protect load diagnostic report: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("publish load diagnostic report: %w", err)
	}
	return nil
}
