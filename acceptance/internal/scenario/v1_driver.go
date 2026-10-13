package scenario

// Native actions for the V1 formal matrix. Browser coverage remains the
// source of truth for the management-page contract; this driver owns bounded
// API actions, service windows, and private durable fact checks. If a frozen
// fault boundary is not controllable by the candidate, the case is recorded
// as infrastructure_failure instead of being declared passed.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Ray-ymq/GoPulse/acceptance/internal/harness"
	"github.com/Ray-ymq/GoPulse/acceptance/internal/receipt"
)

const (
	v1HTTPTimeout       = 12 * time.Second
	v1StageWait         = 60 * time.Second
	v1RecoveryWait      = 120 * time.Second
	v1FaultWindow       = 60 * time.Second
	v1FaultRequests     = 12
	v1FaultRequestEvery = 5 * time.Second
	v1TraceParent       = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
)

type v1HTTPMeta struct {
	CommandID      string    `json:"command_id"`
	EventID        string    `json:"event_id"`
	DataEpoch      string    `json:"data_epoch"`
	PostID         uint64    `json:"post_id"`
	CommittedAt    time.Time `json:"committed_at"`
	Replayed       bool      `json:"replayed"`
	ResourceState  string    `json:"resource_state"`
	idempotencyKey string
}

type v1CreateResult struct {
	Status     int
	Meta       v1HTTPMeta
	Replayed   bool
	ErrorCode  string
	Err        error
	StartedAt  time.Time
	FinishedAt time.Time
}

type v1StageResult struct {
	Resolved struct {
		CommandID string `json:"command_id"`
		EventID   string `json:"event_id"`
		PostID    uint64 `json:"post_id"`
	} `json:"resolved"`
	Evidence struct {
		Command struct {
			Quality string `json:"quality"`
		} `json:"command"`
		Visibility struct {
			Quality string `json:"quality"`
			Matched *bool  `json:"matched"`
		} `json:"visibility"`
		Logs struct {
			Quality string `json:"quality"`
			Entries []struct {
				CommandID string `json:"command_id"`
				DataEpoch string `json:"data_epoch"`
			} `json:"entries"`
		} `json:"logs"`
		Consumers []struct {
			Quality string `json:"quality"`
		} `json:"consumers"`
	} `json:"evidence"`
	Budget struct {
		CurrentOpenLimit        uint64 `json:"current_open_limit"`
		CurrentOpenRequirements uint64 `json:"current_open_requirements"`
	} `json:"budget"`
}

type v1StageHTTPResult struct {
	Status    int
	Stage     v1StageResult
	LogsKnown bool
	Err       error
}

type v1DurableFacts struct {
	Commands         int64
	Posts            int64
	Outbox           int64
	Requirements     int64
	Applications     int64
	BrokerPublished  int64
	OpenRequirements int64
}

type formalV1Runner struct {
	session        *harness.Session
	admin          *v1HTTPClient
	user           *v1HTTPClient
	baseURL        string
	rawDirectory   string
	deadline       time.Time
	commandEpoch   string
	bundle         string
	fixtureBinary  string
	fixtureProcess *exec.Cmd
	fixtureOnce    sync.Once
	fixtureErr     error
	children       map[string]receipt.RawRecord
}

type v1HTTPClient struct {
	baseURL string
	client  *http.Client
}

func newFormalV1Runner(session *harness.Session, rawDirectory, bundle string, deadline time.Time) (*formalV1Runner, error) {
	baseURL, err := composeServiceURL(session, "frontend", "8080")
	if err != nil {
		return nil, err
	}
	admin := newV1HTTPClient(baseURL)
	if err := admin.login(context.Background(), session.Value("GOPULSE_OBSERVABILITY_ADMIN_USERNAME"), session.Value("GOPULSE_OBSERVABILITY_PASSWORD")); err != nil {
		return nil, fmt.Errorf("formal V1 administrator login: %w", err)
	}
	user := newV1HTTPClient(baseURL)
	if err := user.login(context.Background(), session.Value("GOPULSE_OBSERVABILITY_USER_USERNAME"), session.Value("GOPULSE_OBSERVABILITY_PASSWORD")); err != nil {
		return nil, fmt.Errorf("formal V1 ordinary-user login: %w", err)
	}
	epoch, err := admin.commandEpoch(context.Background())
	if err != nil {
		return nil, fmt.Errorf("read V1 command epoch: %w", err)
	}
	return &formalV1Runner{session: session, admin: admin, user: user, baseURL: baseURL, rawDirectory: rawDirectory, deadline: deadline, commandEpoch: epoch, bundle: bundle, children: map[string]receipt.RawRecord{}}, nil
}

func runV1Formal(session *harness.Session, evidence *receipt.V1Evidence, rawDirectory string, deadline time.Time) error {
	runner, err := newFormalV1Runner(session, rawDirectory, filepath.Dir(evidence.Candidate.BundlePath), deadline)
	if err != nil {
		return err
	}
	defer func() { _ = runner.stopFaultProxy() }()
	for _, caseID := range receipt.V1CaseIDs {
		if caseID == "V01" {
			continue
		}
		if err := ensureV1Budget(deadline, "formal "+caseID); err != nil {
			return err
		}
		item, caseErr := runner.runCase(caseID)
		if item.CaseID == "" {
			return fmt.Errorf("formal %s produced no case result", caseID)
		}
		replaceV1Case(evidence, item)
		if caseErr != nil {
			// The frozen contract stops the matrix after the first failed
			// action; later cases remain not_started and cannot be confused
			// with a successful continuation on a damaged stack.
			return caseErr
		}
	}
	return nil
}

func (runner *formalV1Runner) runCase(caseID string) (receipt.V1Case, error) {
	switch caseID {
	case "V02":
		return runner.runV02()
	case "V03":
		return runner.runV03()
	case "V04":
		return runner.runV04()
	case "V05":
		return runner.runV05()
	case "V06":
		return runner.runV06()
	case "V07":
		return runner.runV07()
	case "V08":
		return runner.runV08()
	case "V09":
		return runner.runV09()
	case "V10":
		return runner.runV10()
	case "V11":
		return runner.runV11()
	case "V12":
		return runner.runV12()
	default:
		return receipt.V1Case{}, fmt.Errorf("unknown formal V1 case %s", caseID)
	}
}

func (client *v1HTTPClient) login(ctx context.Context, username, password string) error {
	if strings.TrimSpace(username) == "" || password == "" {
		return errors.New("acceptance account is not configured")
	}
	status, _, err := client.do(ctx, http.MethodPost, "/api/v1/auth/login", nil, map[string]string{"username": username, "password": password})
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("login returned HTTP %d", status)
	}
	return nil
}

func (client *v1HTTPClient) commandEpoch(ctx context.Context) (string, error) {
	status, body, err := client.do(ctx, http.MethodGet, "/api/v1/post-commands/context", nil, nil)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("command context returned HTTP %d", status)
	}
	var envelope struct {
		Data struct {
			DataEpoch string `json:"data_epoch"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Data.DataEpoch == "" {
		return "", errors.New("command context did not return an epoch")
	}
	return envelope.Data.DataEpoch, nil
}

func (client *v1HTTPClient) create(ctx context.Context, epoch, key, title, content, traceparent string) v1CreateResult {
	started := time.Now().UTC()
	headers := map[string]string{"Idempotency-Key": key, "X-GoPulse-Data-Epoch": epoch}
	if traceparent != "" {
		headers["traceparent"] = traceparent
	}
	status, body, err := client.do(ctx, http.MethodPost, "/api/v1/posts", headers, map[string]string{"title": title, "content": content})
	result := v1CreateResult{Status: status, Err: err, StartedAt: started}
	result.FinishedAt = time.Now().UTC()
	if err != nil {
		return result
	}
	var envelope struct {
		Meta struct {
			CommandID     string    `json:"command_id"`
			EventID       string    `json:"event_id"`
			DataEpoch     string    `json:"data_epoch"`
			PostID        uint64    `json:"post_id"`
			CommittedAt   time.Time `json:"committed_at"`
			Replayed      bool      `json:"replayed"`
			ResourceState string    `json:"resource_state"`
		} `json:"meta"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if decodeErr := json.Unmarshal(body, &envelope); decodeErr != nil {
		result.Err = decodeErr
		return result
	}
	result.Meta = v1HTTPMeta{CommandID: envelope.Meta.CommandID, EventID: envelope.Meta.EventID, DataEpoch: envelope.Meta.DataEpoch, PostID: envelope.Meta.PostID, CommittedAt: envelope.Meta.CommittedAt, Replayed: envelope.Meta.Replayed, ResourceState: envelope.Meta.ResourceState, idempotencyKey: key}
	result.Replayed = result.Meta.Replayed
	result.ErrorCode = envelope.Error.Code
	return result
}

func (client *v1HTTPClient) commandResult(ctx context.Context, meta v1HTTPMeta) v1CreateResult {
	started := time.Now().UTC()
	result := v1CreateResult{StartedAt: started}
	if meta.idempotencyKey == "" {
		result.Err = errors.New("command result requires the original idempotency key")
		result.FinishedAt = time.Now().UTC()
		return result
	}
	status, body, err := client.do(ctx, http.MethodGet, "/api/v1/post-commands/result", map[string]string{
		"Idempotency-Key":      meta.idempotencyKey,
		"X-GoPulse-Data-Epoch": meta.DataEpoch,
	}, nil)
	result.Status, result.Err, result.FinishedAt = status, err, time.Now().UTC()
	if err != nil {
		return result
	}
	var envelope struct {
		Meta struct {
			CommandID     string    `json:"command_id"`
			EventID       string    `json:"event_id"`
			DataEpoch     string    `json:"data_epoch"`
			PostID        uint64    `json:"post_id"`
			CommittedAt   time.Time `json:"committed_at"`
			Replayed      bool      `json:"replayed"`
			ResourceState string    `json:"resource_state"`
		} `json:"meta"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if decodeErr := json.Unmarshal(body, &envelope); decodeErr != nil {
		result.Err = decodeErr
		return result
	}
	result.Meta = v1HTTPMeta{CommandID: envelope.Meta.CommandID, EventID: envelope.Meta.EventID, DataEpoch: envelope.Meta.DataEpoch, PostID: envelope.Meta.PostID, CommittedAt: envelope.Meta.CommittedAt, Replayed: envelope.Meta.Replayed, ResourceState: envelope.Meta.ResourceState, idempotencyKey: meta.idempotencyKey}
	result.Replayed = result.Meta.Replayed
	result.ErrorCode = envelope.Error.Code
	return result
}

func (client *v1HTTPClient) currentUserID(ctx context.Context) (uint64, error) {
	status, body, err := client.do(ctx, http.MethodGet, "/api/v1/users/me", nil, nil)
	if err != nil {
		return 0, err
	}
	if status != http.StatusOK {
		return 0, fmt.Errorf("current-user returned HTTP %d", status)
	}
	var envelope struct {
		Data struct {
			ID uint64 `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Data.ID == 0 {
		return 0, errors.New("current-user response did not contain an id")
	}
	return envelope.Data.ID, nil
}

func (client *v1HTTPClient) cookieHeader() string {
	parsed, err := url.Parse(client.baseURL)
	if err != nil || client.client.Jar == nil {
		return ""
	}
	var values []string
	for _, cookie := range client.client.Jar.Cookies(parsed) {
		values = append(values, cookie.Name+"="+cookie.Value)
	}
	return strings.Join(values, "; ")
}

func (client *v1HTTPClient) stage(ctx context.Context, meta v1HTTPMeta, visibility bool) v1StageHTTPResult {
	// committed_at is a durable identity field, not the start of the
	// observability pipeline. Under the formal fault windows the request log
	// and the asynchronous projection can be written on opposite sides of that
	// timestamp. Keep the selector exact, but use the same bounded evidence
	// horizon as the stage service's default query instead of excluding a valid
	// log merely because it precedes the commit by a second.
	from := time.Now().UTC().Add(-15 * time.Minute)
	values := url.Values{}
	values.Set("epoch", meta.DataEpoch)
	values.Set("command_id", meta.CommandID)
	values.Set("check_visibility", strconv.FormatBool(visibility))
	values.Set("include", "logs,trace")
	values.Set("from", from.UTC().Format(time.RFC3339Nano))
	values.Set("to", time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano))
	path := "/api/v1/observability/business-stages?" + values.Encode()
	status, body, err := client.do(ctx, http.MethodGet, path, nil, nil)
	result := v1StageHTTPResult{Status: status, Err: err}
	if err != nil || status != http.StatusOK {
		return result
	}
	var envelope struct {
		Data v1StageResult `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		result.Err = err
		return result
	}
	result.Stage = envelope.Data
	result.LogsKnown = hasPreciseLog(result.Stage, meta)
	return result
}

func hasPreciseLog(stage v1StageResult, meta v1HTTPMeta) bool {
	if stage.Evidence.Logs.Quality != "known" {
		return false
	}
	for _, entry := range stage.Evidence.Logs.Entries {
		if entry.CommandID == meta.CommandID && entry.DataEpoch == meta.DataEpoch {
			return true
		}
	}
	return false
}

func (client *v1HTTPClient) do(ctx context.Context, method, path string, headers map[string]string, payload any) (int, []byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		body = strings.NewReader(string(encoded))
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(client.baseURL, "/")+path, body)
	if err != nil {
		return 0, nil, err
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, 512<<10))
	return response.StatusCode, data, err
}

func newV1HTTPClient(baseURL string) *v1HTTPClient {
	jar, _ := cookiejar.New(nil)
	return &v1HTTPClient{baseURL: strings.TrimRight(baseURL, "/"), client: &http.Client{Jar: jar, Timeout: v1HTTPTimeout}}
}

func (client *v1HTTPClient) withBaseURL(baseURL string) *v1HTTPClient {
	return &v1HTTPClient{baseURL: strings.TrimRight(baseURL, "/"), client: &http.Client{Jar: client.client.Jar, Timeout: v1HTTPTimeout}}
}

func (runner *formalV1Runner) composeTrace(args ...string) error {
	result := runner.session.ComposeFiles([]string{"deploy/phase20-trace.yaml"}, args...)
	if result.ExitCode != 0 {
		return fmt.Errorf("trace overlay Compose failed with exit code %d", result.ExitCode)
	}
	return nil
}

func (runner *formalV1Runner) internalServiceURL(service, port string) (string, error) {
	container, err := runner.containerID(service)
	if err != nil {
		return "", err
	}
	result := runner.session.Run("docker", "inspect", "--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}", container)
	if result.ExitCode != 0 {
		return "", fmt.Errorf("inspect %s address failed with exit code %d", service, result.ExitCode)
	}
	addresses := strings.Fields(result.Stdout)
	if len(addresses) == 0 {
		return "", fmt.Errorf("%s has no container address", service)
	}
	return "http://" + net.JoinHostPort(addresses[0], port), nil
}

func (runner *formalV1Runner) privateComponentMetrics(service, port, token string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("%s metrics token is not configured", service)
	}
	base, err := runner.internalServiceURL(service, port)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequest(http.MethodGet, base+"/internal/v1/metrics", nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return "", err
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s private metrics returned HTTP %d", service, response.StatusCode)
	}
	return string(body), nil
}

func (runner *formalV1Runner) telemetryMetricEvidence() (bool, error) {
	monitor, monitorErr := runner.privateComponentMetrics("monitor", "19104", runner.session.Value("MONITOR_METRICS_TOKEN"))
	router, routerErr := runner.privateComponentMetrics("router", "19105", runner.session.Value("ROUTER_METRICS_TOKEN"))
	if monitorErr != nil || routerErr != nil {
		return false, errors.Join(monitorErr, routerErr)
	}
	return strings.Contains(monitor, "gopulse_monitor_event_queue_dropped_total") || strings.Contains(router, "gopulse_router_backpressure_total"), nil
}

func (runner *formalV1Runner) traceExportPresent() (bool, error) {
	result := runner.session.ComposeFiles([]string{"deploy/phase20-trace.yaml"}, "ps", "-q", "phase20-collector")
	if result.ExitCode != 0 {
		return false, fmt.Errorf("find trace collector failed with exit code %d", result.ExitCode)
	}
	containers := strings.Fields(result.Stdout)
	if len(containers) != 1 {
		return false, fmt.Errorf("expected one trace collector, found %d", len(containers))
	}
	temporary, err := os.CreateTemp(runner.session.TempDir, "trace-export-")
	if err != nil {
		return false, err
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return false, err
	}
	defer func() { _ = os.Remove(temporaryPath) }()
	copyResult := runner.session.Run("docker", "cp", containers[0]+":/var/lib/gopulse/trace/spans.jsonl", temporaryPath)
	if copyResult.ExitCode != 0 {
		// A running collector without a flushed file is an observed absence of
		// export, not a host-permission failure. The formal case reports it as
		// product evidence below.
		return false, nil
	}
	data, err := os.ReadFile(temporaryPath)
	if err != nil {
		return false, err
	}
	return len(bytes.TrimSpace(data)) > 0, nil
}

func composeServiceURL(session *harness.Session, service, port string) (string, error) {
	result := session.Compose("port", service, port)
	if result.ExitCode != 0 {
		return "", fmt.Errorf("resolve %s port failed with exit code %d", service, result.ExitCode)
	}
	fields := strings.Fields(result.Stdout)
	if len(fields) == 0 {
		return "", fmt.Errorf("Compose did not publish %s:%s", service, port)
	}
	endpoint := fields[len(fields)-1]
	host, value, err := net.SplitHostPort(endpoint)
	if err != nil || value == "" {
		return "", fmt.Errorf("invalid published %s endpoint %q", service, endpoint)
	}
	if host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, value), nil
}

func (runner *formalV1Runner) createPrimary(caseID string) (v1HTTPMeta, v1CreateResult, error) {
	key := mustV1UUID()
	title := "Phase-24 " + caseID + " " + key[:8]
	result := runner.admin.create(context.Background(), runner.commandEpoch, key, title, "bounded formal acceptance request for "+caseID, v1TraceParent)
	if result.Status != http.StatusCreated || result.Meta.CommandID == "" {
		return result.Meta, result, fmt.Errorf("%s create returned HTTP %d", caseID, result.Status)
	}
	return result.Meta, result, nil
}

func (runner *formalV1Runner) waitStage(meta v1HTTPMeta, visibility bool, timeout time.Duration) (bool, bool) {
	end := time.Now().Add(timeout)
	if end.After(runner.deadline) {
		end = runner.deadline
	}
	for time.Now().Before(end) {
		result := runner.admin.stage(context.Background(), meta, visibility)
		visibilityOK := !visibility || (result.Stage.Evidence.Visibility.Quality == "known" && result.Stage.Evidence.Visibility.Matched != nil && *result.Stage.Evidence.Visibility.Matched)
		if result.Status == http.StatusOK && result.Stage.Resolved.CommandID == meta.CommandID && result.Stage.Resolved.EventID == meta.EventID && result.Stage.Resolved.PostID == meta.PostID && result.Stage.Evidence.Command.Quality == "known" && hasKnownConsumer(result.Stage) && visibilityOK && result.LogsKnown {
			return true, result.LogsKnown
		}
		time.Sleep(time.Second)
	}
	return false, false
}

func (runner *formalV1Runner) waitAllStages(metas []v1HTTPMeta, timeout time.Duration) bool {
	if len(metas) == 0 {
		return false
	}
	end := time.Now().Add(timeout)
	if end.After(runner.deadline) {
		end = runner.deadline
	}
	for time.Now().Before(end) {
		all := true
		for _, meta := range metas {
			result := runner.admin.stage(context.Background(), meta, true)
			if result.Status != http.StatusOK || result.Stage.Resolved.CommandID != meta.CommandID || result.Stage.Evidence.Command.Quality != "known" || !hasKnownConsumer(result.Stage) || result.Stage.Evidence.Visibility.Quality != "known" || result.Stage.Evidence.Visibility.Matched == nil || !*result.Stage.Evidence.Visibility.Matched || !result.LogsKnown {
				all = false
				break
			}
		}
		if all {
			return true
		}
		time.Sleep(time.Second)
	}
	return false
}

func hasKnownConsumer(stage v1StageResult) bool {
	for _, consumer := range stage.Evidence.Consumers {
		if consumer.Quality == "known" {
			return true
		}
	}
	return false
}

func (runner *formalV1Runner) facts(meta v1HTTPMeta) (v1DurableFacts, error) {
	query := durableFactsQuery(meta)
	command := "MYSQL_PWD=\"$MYSQL_PASSWORD\" mysql --user=\"$MYSQL_USER\" --batch --skip-column-names \"$MYSQL_DATABASE\" --execute " + shellLiteral(query)
	result := runner.session.Compose("exec", "-T", "mysql", "sh", "-ec", command)
	if result.ExitCode != 0 {
		return v1DurableFacts{}, fmt.Errorf("read durable facts failed with exit code %d", result.ExitCode)
	}
	fields := strings.Fields(strings.TrimSpace(result.Stdout))
	if len(fields) != 7 {
		return v1DurableFacts{}, fmt.Errorf("durable fact query returned %d fields", len(fields))
	}
	values := make([]int64, len(fields))
	for index, field := range fields {
		value, err := strconv.ParseInt(field, 10, 64)
		if err != nil || value < 0 {
			return v1DurableFacts{}, fmt.Errorf("durable fact value %q is invalid", field)
		}
		values[index] = value
	}
	return v1DurableFacts{Commands: values[0], Posts: values[1], Outbox: values[2], Requirements: values[3], Applications: values[4], BrokerPublished: values[5], OpenRequirements: values[6]}, nil
}

func (runner *formalV1Runner) projectionPresent(meta v1HTTPMeta) (bool, error) {
	if meta.PostID == 0 {
		return false, errors.New("projection check requires a post identity")
	}
	query := fmt.Sprintf("code=$(curl --silent --show-error --output /dev/null --write-out '%%{http_code}' http://127.0.0.1:9200/gopulse-post-search-v1/_doc/%d); test \"$code\" = 200 || test \"$code\" = 201", meta.PostID)
	result := runner.session.Compose("exec", "-T", "elasticsearch", "sh", "-ec", query)
	if result.ExitCode != 0 {
		return false, nil
	}
	return true, nil
}

func (runner *formalV1Runner) searchGeneration() (string, error) {
	query := "curl --fail --silent http://127.0.0.1:9200/_alias/gopulse-post-search-v1"
	result := runner.session.Compose("exec", "-T", "elasticsearch", "sh", "-ec", query)
	if result.ExitCode != 0 {
		return "", fmt.Errorf("read current Elasticsearch generation failed with exit code %d", result.ExitCode)
	}
	var aliases map[string]json.RawMessage
	if err := json.Unmarshal([]byte(result.Stdout), &aliases); err != nil || len(aliases) != 1 {
		return "", errors.New("current Elasticsearch alias has no single generation")
	}
	for generation := range aliases {
		return generation, nil
	}
	return "", errors.New("current Elasticsearch generation is empty")
}

func (runner *formalV1Runner) waitProjection(meta v1HTTPMeta, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		present, err := runner.projectionPresent(meta)
		if err != nil {
			return false, err
		}
		if present {
			return true, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false, nil
}

func durableFactsQuery(meta v1HTTPMeta) string {
	return fmt.Sprintf("SELECT (SELECT COUNT(*) FROM business_commands WHERE data_epoch=%s AND command_id=%s),(SELECT COUNT(*) FROM posts WHERE id=%d),(SELECT COUNT(*) FROM business_outbox WHERE JSON_UNQUOTE(JSON_EXTRACT(payload,'$.data_epoch'))=%s AND event_id=%s),(SELECT COUNT(*) FROM business_event_requirements WHERE data_epoch=%s AND event_id=%s),(SELECT COUNT(*) FROM business_application_results WHERE data_epoch=%s AND event_id=%s),(SELECT COUNT(*) FROM business_outbox WHERE JSON_UNQUOTE(JSON_EXTRACT(payload,'$.data_epoch'))=%s AND event_id=%s AND status='published'),(SELECT open_requirements FROM business_async_budget WHERE data_epoch=%s)", sqlLiteral(meta.DataEpoch), sqlLiteral(meta.CommandID), meta.PostID, sqlLiteral(meta.DataEpoch), sqlLiteral(meta.EventID), sqlLiteral(meta.DataEpoch), sqlLiteral(meta.EventID), sqlLiteral(meta.DataEpoch), sqlLiteral(meta.EventID), sqlLiteral(meta.DataEpoch), sqlLiteral(meta.EventID), sqlLiteral(meta.DataEpoch))
}

func (runner *formalV1Runner) runV02() (receipt.V1Case, error) {
	started := time.Now().UTC()
	key := mustV1UUID()
	title := "Phase-24 V02 " + key[:8]
	results := make(chan v1CreateResult, 10)
	var wait sync.WaitGroup
	for index := 0; index < 10; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			results <- runner.admin.create(context.Background(), runner.commandEpoch, key, title, "same digest", v1TraceParent)
		}()
	}
	wait.Wait()
	close(results)
	responses := make([]v1CreateResult, 0, 10)
	for result := range results {
		responses = append(responses, result)
	}
	primary, ok := firstSuccessful(responses)
	if !ok {
		identity := receipt.V1Identity{DataEpoch: runner.commandEpoch}
		return runner.failedCase("V02", identity, started, "concurrent same-key requests did not commit", false, nil, 0, 0), errors.New("V02 same-key requests did not commit")
	}
	conflict := runner.admin.create(context.Background(), runner.commandEpoch, key, title+" conflict", "different digest", v1TraceParent)
	otherUser := runner.user.create(context.Background(), runner.commandEpoch, key, title+" other-user", "same digest", v1TraceParent)
	adminID, adminIDErr := runner.admin.currentUserID(context.Background())
	userID, userIDErr := runner.user.currentUserID(context.Background())
	facts, factsErr := runner.facts(primary.Meta)
	otherFacts := v1DurableFacts{}
	var otherFactsErr error
	if otherUser.Status == http.StatusCreated && otherUser.Meta.CommandID != "" && otherUser.Meta.EventID != "" && otherUser.Meta.PostID != 0 {
		otherFacts, otherFactsErr = runner.facts(otherUser.Meta)
	}
	ownerDistinct := adminIDErr == nil && userIDErr == nil && adminID != 0 && userID != 0 && adminID != userID
	otherIdentityDistinct := otherUser.Meta.DataEpoch == primary.Meta.DataEpoch && otherUser.Meta.CommandID != "" && otherUser.Meta.CommandID != primary.Meta.CommandID && otherUser.Meta.EventID != "" && otherUser.Meta.EventID != primary.Meta.EventID && otherUser.Meta.PostID != 0 && otherUser.Meta.PostID != primary.Meta.PostID
	otherFactsValid := otherFactsErr == nil && otherFacts.Commands == 1 && otherFacts.Posts == 1 && otherFacts.Outbox == 1 && otherFacts.Requirements == 1
	factsList := []receipt.RawFact{
		{Name: "requests", Value: int64(len(responses))},
		{Name: "http_success", Value: int64(countStatus(responses, http.StatusCreated))},
		{Name: "conflicts", Value: boolInt(conflict.Status == http.StatusConflict && conflict.ErrorCode == "idempotency_conflict")},
		{Name: "commands", Value: facts.Commands},
		{Name: "posts", Value: facts.Posts},
		{Name: "requirements", Value: facts.Requirements},
		{Name: "unique_events", Value: facts.Outbox},
		{Name: "unique_posts", Value: facts.Posts},
		{Name: "owner_distinct", Value: boolInt(ownerDistinct)},
		{Name: "other_user_created", Value: boolInt(otherUser.Status == http.StatusCreated)},
		{Name: "other_identity_distinct", Value: boolInt(otherIdentityDistinct)},
		{Name: "other_commands", Value: otherFacts.Commands},
		{Name: "other_posts", Value: otherFacts.Posts},
		{Name: "other_requirements", Value: otherFacts.Requirements},
		{Name: "other_events", Value: otherFacts.Outbox},
	}
	item := runner.completedCase("V02", primary.Meta, started, "sent ten same-key requests, checked one durable identity, rejected a different digest, and verified an independent identity for another user", false, false, factsList, 0, 0)
	if factsErr != nil {
		return itemAsFailed(item, "infrastructure_failure", factsErr.Error()), factsErr
	}
	if adminIDErr != nil || userIDErr != nil {
		return itemAsFailed(item, "infrastructure_failure", errors.Join(adminIDErr, userIDErr).Error()), errors.Join(adminIDErr, userIDErr)
	}
	if otherFactsErr != nil {
		return itemAsFailed(item, "infrastructure_failure", otherFactsErr.Error()), otherFactsErr
	}
	passed := countStatus(responses, http.StatusCreated) == 10 && conflict.Status == http.StatusConflict && conflict.ErrorCode == "idempotency_conflict" && otherUser.Status == http.StatusCreated && facts.Commands == 1 && facts.Posts == 1 && facts.Outbox == 1 && facts.Requirements == 1 && ownerDistinct && otherIdentityDistinct && otherFactsValid
	if !passed {
		return itemAsFailed(item, "product_failure", "idempotency or user ownership invariant did not hold"), errors.New("V02 idempotency or ownership invariant did not hold")
	}
	return item, nil
}

func (runner *formalV1Runner) runV03() (receipt.V1Case, error) {
	started := time.Now().UTC()
	key := mustV1UUID()
	title := "Phase-24 V03 " + key[:8]
	proxy, err := runner.startFaultProxy("response-after-commit")
	if err != nil {
		identity := receipt.V1Identity{DataEpoch: runner.commandEpoch}
		return runner.failedCase("V03", identity, started, "start response-loss fixture: "+err.Error(), false, nil, 0, 0), err
	}
	lost := runner.admin.withBaseURL(proxy).create(context.Background(), runner.commandEpoch, key, title, "response-loss request", v1TraceParent)
	stopErr := runner.stopFaultProxy()
	retry := runner.admin.create(context.Background(), runner.commandEpoch, key, title, "response-loss request", v1TraceParent)
	identity := retry.Meta
	if identity.CommandID == "" {
		identity = lost.Meta
	}
	stageOK, logsOK := runner.waitStage(identity, true, v1StageWait)
	facts, factsErr := runner.facts(identity)
	item := runner.completedCase("V03", identity, started, "dropped the HTTP response after a durable commit and retried the same command", stageOK, logsOK, []receipt.RawFact{{Name: "requests", Value: 2}, {Name: "http_success", Value: boolInt(retry.Status == http.StatusCreated)}, {Name: "commands", Value: facts.Commands}, {Name: "posts", Value: facts.Posts}, {Name: "requirements", Value: facts.Requirements}, {Name: "applications", Value: facts.Applications}, {Name: "unique_events", Value: facts.Outbox}}, 0, 0)
	if stopErr != nil {
		return itemAsFailed(item, "infrastructure_failure", "stop response-loss fixture: "+stopErr.Error()), stopErr
	}
	if factsErr != nil {
		return itemAsFailed(item, "infrastructure_failure", factsErr.Error()), factsErr
	}
	if lost.Status != 0 || retry.Status != http.StatusCreated || !retry.Replayed || facts.Commands != 1 || facts.Posts != 1 || facts.Outbox != 1 || facts.Requirements != 1 || !stageOK {
		return itemAsFailed(item, "product_failure", "response-loss retry did not return the original durable result"), errors.New("V03 response-loss retry invariant did not hold")
	}
	control, controlErr := runner.startMySQLControl("commit-result", "backend", "backend-2")
	if controlErr != nil {
		return itemAsFailed(item, "infrastructure_failure", controlErr.Error()), controlErr
	}
	commitKey := mustV1UUID()
	commitResult := runner.admin.create(context.Background(), runner.commandEpoch, commitKey, "Phase-24 V03 COMMIT result", "COMMIT result-loss request", v1TraceParent)
	controlStatus, statusErr := control.waitStatus(v1RecoveryWait, func(value map[string]any) bool {
		return numericStatus(value, "target_queries") >= 1 && numericStatus(value, "upstream_effect") >= 1 && numericStatus(value, "response_dropped") >= 1 && numericStatus(value, "connection_closed") >= 1
	})
	restoreErr := control.Restore()
	commitRetry := runner.admin.create(context.Background(), runner.commandEpoch, commitKey, "Phase-24 V03 COMMIT result", "COMMIT result-loss request", v1TraceParent)
	commitIdentity := commitRetry.Meta
	if commitIdentity.CommandID == "" {
		commitIdentity = commitResult.Meta
	}
	commitStageOK, commitLogsOK := runner.waitStage(commitIdentity, true, v1StageWait)
	commitFacts, commitFactsErr := runner.facts(commitIdentity)
	factsList := []receipt.RawFact{
		{Name: "commit_observed", Value: numericStatus(controlStatus, "upstream_effect")},
		{Name: "commit_result_dropped", Value: numericStatus(controlStatus, "response_dropped")},
		{Name: "mysql_disconnect", Value: numericStatus(controlStatus, "connection_closed")},
		{Name: "http_unavailable", Value: boolInt(commitResult.Status == 0 || commitResult.Status >= http.StatusInternalServerError)},
		{Name: "http_success", Value: boolInt(commitRetry.Status == http.StatusCreated && commitRetry.Replayed)},
		{Name: "response_loss_http_unavailable", Value: boolInt(lost.Status == 0)},
		{Name: "response_loss_replayed", Value: boolInt(retry.Status == http.StatusCreated && retry.Replayed)},
		{Name: "response_loss_unique_events", Value: facts.Outbox},
		{Name: "commands", Value: commitFacts.Commands},
		{Name: "posts", Value: commitFacts.Posts},
		{Name: "requirements", Value: commitFacts.Requirements},
		{Name: "applications", Value: commitFacts.Applications},
		{Name: "unique_events", Value: commitFacts.Outbox},
	}
	commitItem := runner.completedCase("V03", commitIdentity, started, "dropped the HTTP response after commit, then independently dropped the MySQL COMMIT OK packet and replayed the original key", commitStageOK, commitLogsOK, factsList, 0, int64(time.Since(started)/time.Second))
	if statusErr != nil {
		return itemAsFailed(commitItem, "infrastructure_failure", statusErr.Error()), statusErr
	}
	if restoreErr != nil {
		return itemAsFailed(commitItem, "infrastructure_failure", restoreErr.Error()), restoreErr
	}
	if commitFactsErr != nil {
		return itemAsFailed(commitItem, "infrastructure_failure", commitFactsErr.Error()), commitFactsErr
	}
	if controlStatus == nil || stringStatus(controlStatus, "mode") != "commit-result" || numericStatus(controlStatus, "target_queries") < 1 || numericStatus(controlStatus, "upstream_effect") < 1 || numericStatus(controlStatus, "response_dropped") < 1 || numericStatus(controlStatus, "connection_closed") < 1 || commitRetry.Status != http.StatusCreated || !commitRetry.Replayed || commitFacts.Commands != 1 || commitFacts.Posts != 1 || commitFacts.Requirements != 1 || commitFacts.Outbox != 1 || !commitStageOK {
		return itemAsFailed(commitItem, "product_failure", "COMMIT result loss did not preserve one durable command identity"), errors.New("V03 COMMIT result-loss invariant did not hold")
	}
	return commitItem, nil
}

func (runner *formalV1Runner) runV04() (receipt.V1Case, error) {
	started := time.Now().UTC()
	control, err := runner.startAMQPControl("publisher-confirm", "backend", "backend-2")
	if err != nil {
		return runner.failedCase("V04", receipt.V1Identity{DataEpoch: runner.commandEpoch}, started, err.Error(), false, nil, 0, 0), err
	}
	meta, result, createErr := runner.createPrimary("V04")
	if createErr != nil {
		restoreErr := control.Restore()
		if restoreErr != nil {
			createErr = errors.Join(createErr, restoreErr)
		}
		return runner.failedCase("V04", identityFromMeta(meta), started, createErr.Error(), false, nil, 0, 0), createErr
	}
	if err := control.setTarget(meta.EventID); err != nil {
		_ = control.Restore()
		return runner.failedCase("V04", identityFromMeta(meta), started, err.Error(), false, nil, 0, 0), err
	}
	status, statusErr := control.waitStatus(v1RecoveryWait, func(value map[string]any) bool {
		return numericStatus(value, "publisher_confirm_dropped") >= 1 && numericStatus(value, "disconnects") >= 1 && stringStatus(value, "last_event") == meta.EventID
	})
	restoreErr := control.Restore()
	stageOK, logsOK := runner.waitStage(meta, true, v1RecoveryWait)
	facts, factsErr := runner.facts(meta)
	item := runner.completedCase("V04", result.Meta, started, "dropped the real RabbitMQ publisher confirm for the selected event, recorded broker receipt and disconnect, then waited for outbox recovery", stageOK, logsOK, []receipt.RawFact{
		{Name: "publisher_received", Value: numericStatus(status, "publisher_received")},
		{Name: "broker_received", Value: numericStatus(status, "publisher_event_received")},
		{Name: "publisher_confirmed", Value: numericStatus(status, "publisher_confirmed")},
		{Name: "publisher_confirm_dropped", Value: numericStatus(status, "publisher_confirm_dropped")},
		{Name: "broker_confirmed", Value: numericStatus(status, "publisher_confirmed")},
		{Name: "publisher_disconnect", Value: numericStatus(status, "disconnects")},
		{Name: "commands", Value: facts.Commands},
		{Name: "posts", Value: facts.Posts},
		{Name: "requirements", Value: facts.Requirements},
		{Name: "applications", Value: facts.Applications},
		{Name: "broker_published", Value: facts.BrokerPublished},
	}, 0, int64(time.Since(started)/time.Second))
	if createErr != nil {
		return itemAsFailed(item, "infrastructure_failure", createErr.Error()), createErr
	}
	if statusErr != nil {
		return itemAsFailed(item, "infrastructure_failure", statusErr.Error()), statusErr
	}
	if restoreErr != nil {
		return itemAsFailed(item, "infrastructure_failure", restoreErr.Error()), restoreErr
	}
	if factsErr != nil {
		return itemAsFailed(item, "infrastructure_failure", factsErr.Error()), factsErr
	}
	if status == nil || stringStatus(status, "last_event") != meta.EventID || numericStatus(status, "publisher_received") < 1 || numericStatus(status, "publisher_event_received") < 1 || numericStatus(status, "publisher_confirmed") < 1 || numericStatus(status, "publisher_confirm_dropped") < 1 || numericStatus(status, "disconnects") < 1 || facts.Commands != 1 || facts.Posts != 1 || facts.Requirements != 1 || facts.Outbox != 1 || facts.Applications != 1 || !stageOK {
		return itemAsFailed(item, "product_failure", "publisher confirm loss did not recover the selected event exactly once"), errors.New("V04 publisher confirm invariant did not hold")
	}
	return item, nil
}

func (runner *formalV1Runner) runV05() (receipt.V1Case, error) {
	started := time.Now().UTC()
	esControl, err := runner.startElasticsearchControl("effect-before-completion", "backend", "backend-2", "search-indexer", "search-indexer-2")
	if err != nil {
		return runner.failedCase("V05", receipt.V1Identity{DataEpoch: runner.commandEpoch}, started, err.Error(), false, nil, 0, 0), err
	}
	esMeta, esResult, esCreateErr := runner.createPrimary("V05")
	if esCreateErr != nil {
		restoreErr := esControl.Restore()
		if restoreErr != nil {
			esCreateErr = errors.Join(esCreateErr, restoreErr)
		}
		return runner.failedCase("V05", identityFromMeta(esMeta), started, esCreateErr.Error(), false, nil, 0, 0), esCreateErr
	}
	esStatus, esStatusErr := esControl.waitStatus(v1RecoveryWait, func(value map[string]any) bool {
		return numericStatus(value, "effect_observed") >= 1 && numericStatus(value, "response_dropped") >= 1 && numericStatus(value, "connection_closed") >= 1
	})
	esStopErr := runner.compose("stop", "search-indexer", "search-indexer-2")
	esProjection, esProjectionErr := runner.waitProjection(esMeta, 30*time.Second)
	esFacts, esFactsErr := runner.facts(esMeta)
	esRestoreErr := esControl.Restore()
	esStageOK, esLogsOK := runner.waitStage(esMeta, true, v1RecoveryWait)
	esRecoveredFacts, esRecoveredFactsErr := runner.facts(esMeta)
	esItem := runner.completedCase("V05", esResult.Meta, started, "dropped the successful ES write response after the projection was written, stopped the indexers before completion could be recorded, and replayed the event", esStageOK, esLogsOK, []receipt.RawFact{
		{Name: "projection_present", Value: boolInt(esProjection)},
		{Name: "completion_present", Value: esFacts.Applications},
		{Name: "completion_blocked", Value: boolInt(esFacts.Applications == 0)},
		{Name: "es_effect_observed", Value: numericStatus(esStatus, "effect_observed")},
		{Name: "es_response_dropped", Value: numericStatus(esStatus, "response_dropped")},
		{Name: "http_unavailable", Value: boolInt(numericStatus(esStatus, "response_dropped") >= 1)},
		{Name: "commands", Value: esFacts.Commands},
		{Name: "posts", Value: esFacts.Posts},
		{Name: "requirements", Value: esFacts.Requirements},
		{Name: "applications", Value: esRecoveredFacts.Applications},
		{Name: "unique_events", Value: esFacts.Outbox},
	}, 0, int64(time.Since(started)/time.Second))
	for _, failure := range []struct {
		err  error
		name string
	}{
		{esStatusErr, "ES control"}, {esStopErr, "ES stop"}, {esProjectionErr, "ES projection"}, {esFactsErr, "ES facts"}, {esRestoreErr, "ES restore"}, {esRecoveredFactsErr, "ES recovered facts"},
	} {
		if failure.err != nil {
			return itemAsFailed(esItem, "infrastructure_failure", failure.name+": "+failure.err.Error()), failure.err
		}
	}
	if !esProjection || esFacts.Applications != 0 || esRecoveredFacts.Applications != 1 || !esStageOK {
		return itemAsFailed(esItem, "product_failure", "ES effect/response loss did not preserve projection-before-completion evidence"), errors.New("V05 ES effect/response invariant did not hold")
	}

	mysqlControl, mysqlErr := runner.startMySQLControl("completion-write", "search-indexer", "search-indexer-2")
	if mysqlErr != nil {
		return itemAsFailed(esItem, "infrastructure_failure", mysqlErr.Error()), mysqlErr
	}
	mysqlMeta, _, mysqlCreateErr := runner.createPrimary("V05")
	if mysqlCreateErr != nil {
		restoreErr := mysqlControl.Restore()
		if restoreErr != nil {
			mysqlCreateErr = errors.Join(mysqlCreateErr, restoreErr)
		}
		return itemAsFailed(esItem, "infrastructure_failure", mysqlCreateErr.Error()), mysqlCreateErr
	}
	mysqlStatus, mysqlStatusErr := mysqlControl.waitStatus(v1RecoveryWait, func(value map[string]any) bool {
		return numericStatus(value, "upstream_effect") >= 1 && numericStatus(value, "response_dropped") >= 1 && numericStatus(value, "connection_closed") >= 1
	})
	mysqlStopErr := runner.compose("stop", "search-indexer", "search-indexer-2")
	mysqlProjection, mysqlProjectionErr := runner.waitProjection(mysqlMeta, 30*time.Second)
	mysqlFacts, mysqlFactsErr := runner.facts(mysqlMeta)
	mysqlRestoreErr := mysqlControl.Restore()
	mysqlStageOK, mysqlLogsOK := runner.waitStage(mysqlMeta, true, v1RecoveryWait)
	mysqlRecoveredFacts, mysqlRecoveredFactsErr := runner.facts(mysqlMeta)

	identity := mysqlMeta
	if identity.CommandID == "" {
		identity = esMeta
	}
	item := runner.completedCase("V05", identity, started, "separately dropped the successful ES write response and the completion INSERT response, proved projection-before-completion in both windows, then replayed both events", esStageOK && mysqlStageOK, esLogsOK && mysqlLogsOK, []receipt.RawFact{
		{Name: "projection_present", Value: boolInt(esProjection && mysqlProjection)},
		{Name: "completion_present", Value: esFacts.Applications + mysqlFacts.Applications},
		{Name: "completion_blocked", Value: boolInt(esFacts.Applications == 0 && mysqlFacts.Applications == 0)},
		{Name: "es_effect_observed", Value: numericStatus(esStatus, "effect_observed")},
		{Name: "es_response_dropped", Value: numericStatus(esStatus, "response_dropped")},
		{Name: "completion_effect_observed", Value: numericStatus(mysqlStatus, "upstream_effect")},
		{Name: "completion_response_dropped", Value: numericStatus(mysqlStatus, "response_dropped")},
		{Name: "completion_disconnect", Value: numericStatus(mysqlStatus, "connection_closed")},
		{Name: "http_unavailable", Value: boolInt(numericStatus(esStatus, "response_dropped") >= 1)},
		{Name: "commands", Value: esFacts.Commands + mysqlFacts.Commands},
		{Name: "posts", Value: esFacts.Posts + mysqlFacts.Posts},
		{Name: "requirements", Value: esFacts.Requirements + mysqlFacts.Requirements},
		{Name: "applications", Value: esRecoveredFacts.Applications + mysqlRecoveredFacts.Applications},
		{Name: "unique_events", Value: esFacts.Outbox + mysqlFacts.Outbox},
	}, 0, int64(time.Since(started)/time.Second))
	for _, failure := range []struct {
		err  error
		name string
	}{
		{esCreateErr, "ES create"}, {esStatusErr, "ES control"}, {esStopErr, "ES stop"}, {esProjectionErr, "ES projection"}, {esFactsErr, "ES facts"}, {esRestoreErr, "ES restore"}, {esRecoveredFactsErr, "ES recovered facts"},
		{mysqlCreateErr, "completion create"}, {mysqlStatusErr, "completion control"}, {mysqlStopErr, "completion stop"}, {mysqlProjectionErr, "completion projection"}, {mysqlFactsErr, "completion facts"}, {mysqlRestoreErr, "completion restore"}, {mysqlRecoveredFactsErr, "completion recovered facts"},
	} {
		if failure.err != nil {
			return itemAsFailed(item, "infrastructure_failure", failure.name+": "+failure.err.Error()), failure.err
		}
	}
	if !esProjection || esFacts.Applications != 0 || !mysqlProjection || mysqlFacts.Applications != 0 || esRecoveredFacts.Applications != 1 || mysqlRecoveredFacts.Applications != 1 || numericStatus(esStatus, "effect_observed") < 1 || numericStatus(mysqlStatus, "upstream_effect") < 1 || numericStatus(mysqlStatus, "response_dropped") < 1 || !esStageOK || !mysqlStageOK {
		return itemAsFailed(item, "product_failure", "ES effect and completion persistence gaps did not leave the required projection-before-record evidence"), errors.New("V05 effect/completion gap invariant did not hold")
	}
	return item, nil
}

func (runner *formalV1Runner) runBoundary(caseID, service, action, missing string) (receipt.V1Case, error) {
	started := time.Now().UTC()
	services := []string{service}
	if service == "search-indexer" {
		services = []string{"search-indexer", "search-indexer-2"}
	}
	stopArgs := append([]string{"stop"}, services...)
	if err := runner.compose(stopArgs...); err != nil {
		identity := receipt.V1Identity{DataEpoch: runner.commandEpoch}
		return runner.failedCase(caseID, identity, started, "stop "+service+": "+err.Error(), false, nil, 0, 0), err
	}
	meta, result, createErr := runner.createPrimary(caseID)
	upArgs := append([]string{"up", "--detach", "--wait", "--wait-timeout", "420"}, services...)
	recoverErr := runner.compose(upArgs...)
	stageOK, logsOK := runner.waitStage(meta, true, v1RecoveryWait)
	facts, factsErr := runner.facts(meta)
	item := runner.completedCase(caseID, result.Meta, started, action, stageOK, logsOK, []receipt.RawFact{{Name: "commands", Value: facts.Commands}, {Name: "posts", Value: facts.Posts}, {Name: "requirements", Value: facts.Requirements}, {Name: "applications", Value: facts.Applications}, {Name: "broker_published", Value: facts.BrokerPublished}}, 0, int64(time.Since(started)/time.Second))
	if createErr != nil {
		return itemAsFailed(item, "infrastructure_failure", createErr.Error()), createErr
	}
	if recoverErr != nil {
		return itemAsFailed(item, "infrastructure_failure", "service recovery failed: "+recoverErr.Error()), recoverErr
	}
	if factsErr != nil {
		return itemAsFailed(item, "infrastructure_failure", factsErr.Error()), factsErr
	}
	if !stageOK {
		return itemAsFailed(item, "product_failure", "search completion did not recover after service returned"), errors.New("service boundary recovery failed")
	}
	return itemAsFailed(item, "infrastructure_failure", missing), errors.New(missing)
}

func (runner *formalV1Runner) runV06() (receipt.V1Case, error) {
	started := time.Now().UTC()
	if err := runner.compose("stop", "search-indexer", "search-indexer-2"); err != nil {
		return runner.failedCase("V06", receipt.V1Identity{DataEpoch: runner.commandEpoch}, started, err.Error(), false, nil, 0, 0), err
	}
	control, err := runner.startAMQPControl("consumer-ack")
	if err != nil {
		return runner.failedCase("V06", receipt.V1Identity{DataEpoch: runner.commandEpoch}, started, err.Error(), false, nil, 0, 0), err
	}
	meta, result, createErr := runner.createPrimary("V06")
	if createErr != nil {
		restoreErr := control.Restore()
		if restoreErr != nil {
			createErr = errors.Join(createErr, restoreErr)
		}
		return runner.failedCase("V06", identityFromMeta(meta), started, createErr.Error(), false, nil, 0, 0), createErr
	}
	if err := control.setTarget(meta.EventID); err != nil {
		_ = control.Restore()
		return runner.failedCase("V06", identityFromMeta(meta), started, err.Error(), false, nil, 0, 0), err
	}
	if err := runner.compose("up", "--detach", "--wait", "--wait-timeout", "420", "search-indexer", "search-indexer-2"); err != nil {
		_ = control.Restore()
		return runner.failedCase("V06", identityFromMeta(meta), started, err.Error(), false, nil, 0, 0), err
	}
	status, statusErr := control.waitStatus(v1RecoveryWait, func(value map[string]any) bool {
		return numericStatus(value, "consumer_delivery_received") >= 1 && numericStatus(value, "consumer_ack_dropped") >= 1
	})
	completionBeforeRestore, completionErr := runner.facts(meta)
	redeliveryStatus, redeliveryErr := control.waitStatus(v1RecoveryWait, func(value map[string]any) bool {
		return numericStatus(value, "consumer_redelivered") >= 1 && numericStatus(value, "consumer_redelivery_ack") >= 1
	})
	disconnects := numericStatus(status, "disconnects")
	if current := numericStatus(redeliveryStatus, "disconnects"); current > disconnects {
		disconnects = current
	}
	restoreErr := control.Restore()
	stageOK, logsOK := runner.waitStage(meta, true, v1RecoveryWait)
	facts, factsErr := runner.facts(meta)
	item := runner.completedCase("V06", result.Meta, started, "dropped the real consumer acknowledgement after completion persistence, observed disconnect and RabbitMQ redelivery, then forwarded the redelivery acknowledgement", stageOK, logsOK, []receipt.RawFact{
		{Name: "consumer_delivery_received", Value: numericStatus(status, "consumer_delivery_received")},
		{Name: "consumer_ack_dropped", Value: numericStatus(status, "consumer_ack_dropped")},
		{Name: "consumer_redelivered", Value: numericStatus(redeliveryStatus, "consumer_redelivered")},
		{Name: "consumer_redelivery_ack", Value: numericStatus(redeliveryStatus, "consumer_redelivery_ack")},
		{Name: "consumer_disconnect", Value: disconnects},
		{Name: "commands", Value: facts.Commands},
		{Name: "posts", Value: facts.Posts},
		{Name: "applications", Value: facts.Applications},
		{Name: "redelivered", Value: numericStatus(redeliveryStatus, "consumer_redelivered")},
	}, 0, int64(time.Since(started)/time.Second))
	if createErr != nil {
		return itemAsFailed(item, "infrastructure_failure", createErr.Error()), createErr
	}
	for _, failure := range []struct {
		err  error
		name string
	}{
		{statusErr, "initial acknowledgement control"}, {completionErr, "completion facts"}, {redeliveryErr, "redelivery control"}, {restoreErr, "restore"}, {factsErr, "recovered facts"},
	} {
		if failure.err != nil {
			return itemAsFailed(item, "infrastructure_failure", failure.name+": "+failure.err.Error()), failure.err
		}
	}
	if completionBeforeRestore.Applications != 1 || facts.Applications != 1 || numericStatus(status, "consumer_ack_dropped") < 1 || numericStatus(redeliveryStatus, "consumer_redelivered") < 1 || numericStatus(redeliveryStatus, "consumer_redelivery_ack") < 1 || disconnects < 1 || !stageOK {
		return itemAsFailed(item, "product_failure", "consumer acknowledgement loss did not preserve one completion and one redelivery"), errors.New("V06 acknowledgement-loss invariant did not hold")
	}
	return item, nil
}

func (runner *formalV1Runner) runV07() (receipt.V1Case, error) {
	return runner.runSearchWindows("V07", "held Elasticsearch unavailable and paused windows for sixty seconds with twelve creates each")
}

func (runner *formalV1Runner) runSearchWindows(caseID, action string) (receipt.V1Case, error) {
	started := time.Now().UTC()
	var metas []v1HTTPMeta
	var primary v1HTTPMeta
	var success int64
	var unavailable int64
	for window := 0; window < 2; window++ {
		if window == 0 {
			if result := runner.session.Compose("stop", "elasticsearch"); result.ExitCode != 0 {
				err := fmt.Errorf("stop Elasticsearch failed with exit code %d", result.ExitCode)
				return runner.failedCase(caseID, primaryIdentity(primary, runner.commandEpoch), started, err.Error(), false, nil, int64(time.Since(started)/time.Second), 0), err
			}
		} else {
			if result := runner.session.Compose("up", "--detach", "--wait", "--wait-timeout", "420", "elasticsearch"); result.ExitCode != 0 {
				err := fmt.Errorf("restore Elasticsearch before delayed window failed with exit code %d", result.ExitCode)
				return runner.failedCase(caseID, primaryIdentity(primary, runner.commandEpoch), started, err.Error(), false, nil, int64(time.Since(started)/time.Second), 0), err
			}
			if result := runner.session.Compose("pause", "elasticsearch"); result.ExitCode != 0 {
				err := fmt.Errorf("pause Elasticsearch failed with exit code %d", result.ExitCode)
				return runner.failedCase(caseID, primaryIdentity(primary, runner.commandEpoch), started, err.Error(), false, nil, int64(time.Since(started)/time.Second), 0), err
			}
		}
		// The clock starts only after the dependency is actually unavailable or
		// paused. Startup and pause time are control overhead, not evidence for
		// the required sixty-second fault window.
		windowStarted := time.Now().UTC()
		for index := 0; index < v1FaultRequests; index++ {
			if index > 0 {
				if err := waitUntil(runner.deadline, windowStarted.Add(time.Duration(index)*v1FaultRequestEvery)); err != nil {
					return runner.failedCase(caseID, primaryIdentity(primary, runner.commandEpoch), started, err.Error(), false, nil, int64(time.Since(started)/time.Second), 0), err
				}
			}
			result := runner.admin.create(context.Background(), runner.commandEpoch, mustV1UUID(), fmt.Sprintf("%s window %d request %d", caseID, window, index), "bounded search fault request", v1TraceParent)
			if result.Status == 0 || result.Status >= http.StatusInternalServerError {
				unavailable++
			}
			if result.Status == http.StatusCreated {
				success++
				metas = append(metas, result.Meta)
				if primary.CommandID == "" {
					primary = result.Meta
				}
			}
		}
		if err := waitUntil(runner.deadline, windowStarted.Add(v1FaultWindow)); err != nil {
			return runner.failedCase(caseID, primaryIdentity(primary, runner.commandEpoch), started, err.Error(), false, nil, int64(time.Since(started)/time.Second), 0), err
		}
		if window == 1 {
			if result := runner.session.Compose("unpause", "elasticsearch"); result.ExitCode != 0 {
				err := fmt.Errorf("unpause Elasticsearch failed with exit code %d", result.ExitCode)
				return runner.failedCase(caseID, primaryIdentity(primary, runner.commandEpoch), started, err.Error(), false, nil, int64(time.Since(started)/time.Second), 0), err
			}
		}
		if result := runner.session.Compose("up", "--detach", "--wait", "--wait-timeout", "420", "elasticsearch"); result.ExitCode != 0 {
			err := fmt.Errorf("restore Elasticsearch after window failed with exit code %d", result.ExitCode)
			return runner.failedCase(caseID, primaryIdentity(primary, runner.commandEpoch), started, err.Error(), false, nil, int64(time.Since(started)/time.Second), 0), err
		}
	}
	recovered := runner.waitAllStages(metas, v1RecoveryWait)
	facts := v1DurableFacts{}
	var factsErr error
	for _, meta := range metas {
		current, err := runner.facts(meta)
		if err != nil {
			factsErr = errors.Join(factsErr, err)
			continue
		}
		facts.Commands += current.Commands
		facts.Posts += current.Posts
		facts.Outbox += current.Outbox
		facts.Requirements += current.Requirements
		facts.Applications += current.Applications
		facts.BrokerPublished += current.BrokerPublished
	}
	item := runner.completedCase(caseID, primary, started, action+"; first subwindow unavailable and second subwindow delayed", recovered, recovered, []receipt.RawFact{{Name: "fault_requests", Value: int64(v1FaultRequests * 2)}, {Name: "fault_windows", Value: 2}, {Name: "http_success", Value: success}, {Name: "http_unavailable", Value: unavailable}, {Name: "requests", Value: int64(v1FaultRequests * 2)}, {Name: "commands", Value: facts.Commands}, {Name: "posts", Value: facts.Posts}, {Name: "requirements", Value: facts.Requirements}, {Name: "applications", Value: facts.Applications}, {Name: "business_independent", Value: boolInt(success == int64(v1FaultRequests*2))}}, 120, int64(time.Since(started)/time.Second))
	if factsErr != nil {
		return itemAsFailed(item, "infrastructure_failure", "durable facts: "+factsErr.Error()), factsErr
	}
	if success != int64(v1FaultRequests*2) || facts.Commands != success || facts.Posts != success || facts.Requirements != success || facts.Applications != success || !recovered {
		return itemAsFailed(item, "product_failure", "business create or search recovery failed during the required windows"), errors.New(caseID + " fault windows failed")
	}
	return item, nil
}

func (runner *formalV1Runner) runV08() (receipt.V1Case, error) {
	started := time.Now().UTC()
	defer runner.session.SetValue("BUSINESS_ASYNC_MAX_OPEN_REQUIREMENTS", "10000")
	if err := runner.compose("stop", "search-indexer", "search-indexer-2"); err != nil {
		return runner.failedCase("V08", receipt.V1Identity{DataEpoch: runner.commandEpoch}, started, err.Error(), false, nil, 0, 0), err
	}
	runner.session.SetValue("BUSINESS_ASYNC_MAX_OPEN_REQUIREMENTS", "8")
	if err := runner.compose("up", "--detach", "--wait", "--wait-timeout", "420", "--force-recreate", "--no-deps", "backend", "backend-2", "business-worker", "business-worker-2"); err != nil {
		return runner.failedCase("V08", receipt.V1Identity{DataEpoch: runner.commandEpoch}, started, err.Error(), false, nil, 0, 0), err
	}
	metas := make([]v1HTTPMeta, 0, 8)
	for index := 0; index < 8; index++ {
		meta, result, createErr := runner.createPrimary("V08")
		if createErr != nil || result.Status != http.StatusCreated {
			identity := identityFromMeta(meta)
			if identity.CommandID == "" && len(metas) > 0 {
				identity = identityFromMeta(metas[0])
			}
			return runner.failedCase("V08", identity, started, fmt.Sprintf("real obligation %d was not admitted: HTTP %d", index+1, result.Status), false, nil, 0, 0), fmt.Errorf("V08 obligation %d was not admitted", index+1)
		}
		metas = append(metas, meta)
	}
	first := metas[0]
	_, beforeErr := runner.facts(first)
	budgetResponse := runner.admin.stage(context.Background(), first, false)
	lowWaterLimit := int64(budgetResponse.Stage.Budget.CurrentOpenLimit)
	ninth := runner.admin.create(context.Background(), runner.commandEpoch, mustV1UUID(), "Phase-24 V08 rejected", "the ninth real obligation must be rejected", v1TraceParent)
	rejectedFacts, rejectedFactsErr := runner.facts(first)
	if err := runner.compose("up", "--detach", "--wait", "--wait-timeout", "420", "--force-recreate", "--no-deps", "search-indexer", "search-indexer-2"); err != nil {
		return runner.failedCase("V08", identityFromMeta(first), started, err.Error(), false, nil, 0, 0), err
	}
	stageOK := runner.waitAllStages(metas, v1RecoveryWait)
	after, afterErr := runner.facts(first)
	totalApplications := int64(0)
	for _, meta := range metas {
		completed, factsErr := runner.facts(meta)
		if factsErr == nil {
			totalApplications += completed.Applications
		}
	}
	recoveryMeta, recoveryResult, recoveryErr := runner.createPrimary("V08")
	recoveryStageOK, recoveryLogsOK := false, false
	if recoveryErr == nil {
		recoveryStageOK, recoveryLogsOK = runner.waitStage(recoveryMeta, true, v1StageWait)
	}
	runner.session.SetValue("BUSINESS_ASYNC_MAX_OPEN_REQUIREMENTS", "10000")
	restoreDefaultErr := runner.compose("up", "--detach", "--wait", "--wait-timeout", "420", "--force-recreate", "--no-deps", "backend", "backend-2", "business-worker", "business-worker-2", "search-indexer", "search-indexer-2")
	if recoveryErr != nil {
		recoveryStageOK, recoveryLogsOK = false, false
	}
	if recoveryMeta.CommandID == "" {
		recoveryMeta = first
	}
	item := runner.completedCase("V08", first, started, "set the active environment limit to eight, filled eight real requirements with indexers stopped, rejected the ninth request at HTTP 503, drained the real backlog, and restored the production default", stageOK && recoveryStageOK && recoveryLogsOK, recoveryLogsOK, []receipt.RawFact{
		{Name: "low_water_limit", Value: lowWaterLimit},
		{Name: "open_requirements_before", Value: rejectedFacts.OpenRequirements},
		{Name: "open_requirements_after", Value: after.OpenRequirements},
		{Name: "rejected", Value: boolInt(ninth.Status == http.StatusServiceUnavailable && ninth.ErrorCode == "business_async_backlog")},
		{Name: "http_unavailable", Value: boolInt(ninth.Status == http.StatusServiceUnavailable)},
		{Name: "requests", Value: 9},
		{Name: "commands", Value: rejectedFacts.Commands},
		{Name: "posts", Value: rejectedFacts.Posts},
		{Name: "requirements", Value: rejectedFacts.Requirements},
		{Name: "applications", Value: totalApplications},
	}, 0, int64(time.Since(started)/time.Second))
	for _, failure := range []struct {
		err  error
		name string
	}{
		{beforeErr, "pre-drain facts"}, {rejectedFactsErr, "rejection facts"}, {afterErr, "drain facts"}, {recoveryErr, "recovery create"}, {restoreDefaultErr, "restore default"},
	} {
		if failure.err != nil {
			return itemAsFailed(item, "infrastructure_failure", failure.name+": "+failure.err.Error()), failure.err
		}
	}
	if lowWaterLimit != 8 || rejectedFacts.OpenRequirements != 8 || ninth.Status != http.StatusServiceUnavailable || ninth.ErrorCode != "business_async_backlog" || after.OpenRequirements != 0 || totalApplications != 8 || recoveryResult.Status != http.StatusCreated || !stageOK || !recoveryStageOK || !recoveryLogsOK {
		return itemAsFailed(item, "product_failure", "eight real obligations did not enforce and drain the configured low-water boundary"), errors.New("V08 low-water admission invariant did not hold")
	}
	return item, nil
}

func (runner *formalV1Runner) runV09() (receipt.V1Case, error) {
	started := time.Now().UTC()
	if err := runner.composeTrace("up", "--detach", "--wait", "--wait-timeout", "420", "phase20-collector", "backend", "backend-2", "business-worker", "business-worker-2", "search-indexer", "search-indexer-2"); err != nil {
		return runner.failedCase("V09", receipt.V1Identity{DataEpoch: runner.commandEpoch}, started, "start pinned OTLP/Collector overlay: "+err.Error(), false, nil, 0, 0), err
	}
	// The overlay is acceptance-owned. Remove it and force the normal candidate
	// definitions back before V10 starts; otherwise trace-enabled environment
	// values would silently leak into the following case.
	traceRestored := false
	restoreTrace := func() error {
		if traceRestored {
			return nil
		}
		var first error
		if result := runner.session.ComposeFiles([]string{"deploy/phase20-trace.yaml"}, "rm", "-sf", "phase20-collector"); result.ExitCode != 0 {
			first = fmt.Errorf("remove trace collector failed with exit code %d", result.ExitCode)
		}
		if result := runner.session.Compose("up", "--detach", "--wait", "--wait-timeout", "420", "--force-recreate", "--no-deps", "backend", "backend-2", "business-worker", "business-worker-2", "search-indexer", "search-indexer-2"); result.ExitCode != 0 && first == nil {
			first = fmt.Errorf("restore normal telemetry environment failed with exit code %d", result.ExitCode)
		}
		if first == nil {
			traceRestored = true
		}
		return first
	}
	defer func() { _ = restoreTrace() }()
	windows := []struct {
		name     string
		services []string
	}{
		{name: "collector", services: []string{"phase20-collector"}},
		{name: "router", services: []string{"router", "router-2"}},
		{name: "observability-elasticsearch", services: []string{"observability-elasticsearch"}},
	}
	var metas []v1HTTPMeta
	var primary v1HTTPMeta
	var success int64
	var unavailable int64
	for _, window := range windows {
		if result := runner.session.ComposeFiles([]string{"deploy/phase20-trace.yaml"}, append([]string{"stop"}, window.services...)...); result.ExitCode != 0 {
			err := fmt.Errorf("stop %s: Compose exit code %d", window.name, result.ExitCode)
			return runner.failedCase("V09", primaryIdentity(primary, runner.commandEpoch), started, err.Error(), false, nil, int64(time.Since(started)/time.Second), 0), err
		}
		windowStarted := time.Now().UTC()
		for index := 0; index < v1FaultRequests; index++ {
			if index > 0 {
				if err := waitUntil(runner.deadline, windowStarted.Add(time.Duration(index)*v1FaultRequestEvery)); err != nil {
					return runner.failedCase("V09", primaryIdentity(primary, runner.commandEpoch), started, err.Error(), false, nil, int64(time.Since(started)/time.Second), 0), err
				}
			}
			result := runner.admin.create(context.Background(), runner.commandEpoch, mustV1UUID(), fmt.Sprintf("V09 %s %d", window.name, index), "bounded telemetry fault request", v1TraceParent)
			if result.Status == 0 || result.Status >= http.StatusInternalServerError {
				unavailable++
			}
			if result.Status == http.StatusCreated {
				success++
				metas = append(metas, result.Meta)
				if primary.CommandID == "" {
					primary = result.Meta
				}
			}
		}
		if err := waitUntil(runner.deadline, windowStarted.Add(v1FaultWindow)); err != nil {
			return runner.failedCase("V09", primaryIdentity(primary, runner.commandEpoch), started, err.Error(), false, nil, int64(time.Since(started)/time.Second), 0), err
		}
		if result := runner.session.ComposeFiles([]string{"deploy/phase20-trace.yaml"}, append([]string{"up", "--detach", "--wait", "--wait-timeout", "420"}, window.services...)...); result.ExitCode != 0 {
			err := fmt.Errorf("recover %s: Compose exit code %d", window.name, result.ExitCode)
			return runner.failedCase("V09", primaryIdentity(primary, runner.commandEpoch), started, err.Error(), false, nil, int64(time.Since(started)/time.Second), 0), err
		}
	}
	recovered := runner.waitAllStages(metas, v1RecoveryWait)
	facts := v1DurableFacts{}
	var factsErr error
	for _, meta := range metas {
		current, err := runner.facts(meta)
		if err != nil {
			factsErr = errors.Join(factsErr, err)
			continue
		}
		facts.Commands += current.Commands
		facts.Posts += current.Posts
		facts.Outbox += current.Outbox
		facts.Requirements += current.Requirements
		facts.Applications += current.Applications
	}
	traceExported, traceErr := runner.traceExportPresent()
	metricEvidence, metricErr := runner.telemetryMetricEvidence()
	restoreErr := restoreTrace()
	item := runner.completedCase("V09", primary, started, "held the pinned OTLP/Collector, Monitor-to-Router, and observability Logs Elasticsearch fault windows with twelve business requests each", recovered, recovered, []receipt.RawFact{{Name: "fault_requests", Value: int64(len(windows) * v1FaultRequests)}, {Name: "fault_windows", Value: int64(len(windows))}, {Name: "http_success", Value: success}, {Name: "http_unavailable", Value: unavailable}, {Name: "requests", Value: int64(len(windows) * v1FaultRequests)}, {Name: "commands", Value: facts.Commands}, {Name: "posts", Value: facts.Posts}, {Name: "requirements", Value: facts.Requirements}, {Name: "applications", Value: facts.Applications}, {Name: "business_independent", Value: boolInt(success == int64(len(windows)*v1FaultRequests) && facts.Applications == success)}, {Name: "telemetry_export_observed", Value: boolInt(traceExported)}, {Name: "telemetry_retry_observed", Value: boolInt(metricEvidence)}, {Name: "telemetry_drop_observed", Value: boolInt(metricEvidence)}, {Name: "telemetry_unknown", Value: boolInt(!metricEvidence)}}, 180, int64(time.Since(started)/time.Second))
	if factsErr != nil {
		return itemAsFailed(item, "infrastructure_failure", "durable facts: "+factsErr.Error()), factsErr
	}
	if traceErr != nil {
		return itemAsFailed(item, "infrastructure_failure", "trace export evidence: "+traceErr.Error()), traceErr
	}
	if metricErr != nil {
		return itemAsFailed(item, "infrastructure_failure", "telemetry metric evidence: "+metricErr.Error()), metricErr
	}
	if restoreErr != nil {
		return itemAsFailed(item, "infrastructure_failure", "restore normal telemetry environment: "+restoreErr.Error()), restoreErr
	}
	if success != int64(len(windows)*v1FaultRequests) || facts.Commands != success || facts.Posts != success || facts.Requirements != success || facts.Applications != success || !recovered || !traceExported {
		return itemAsFailed(item, "product_failure", "business writes or completion recovery did not remain available during telemetry faults"), errors.New("V09 telemetry fault recovery failed")
	}
	return item, nil
}

func (runner *formalV1Runner) runV10() (receipt.V1Case, error) {
	started := time.Now().UTC()
	if err := runner.compose("stop", "platform-api"); err != nil {
		return runner.failedCase("V10", primaryIdentity(v1HTTPMeta{}, runner.commandEpoch), started, err.Error(), false, nil, 0, 0), err
	}
	var metas []v1HTTPMeta
	var primary v1HTTPMeta
	var success int64
	windowStarted := time.Now()
	for index := 0; index < v1FaultRequests; index++ {
		if index > 0 {
			if err := waitUntil(runner.deadline, windowStarted.Add(time.Duration(index)*v1FaultRequestEvery)); err != nil {
				return runner.failedCase("V10", primaryIdentity(primary, runner.commandEpoch), started, err.Error(), false, nil, int64(time.Since(started)/time.Second), 0), err
			}
		}
		result := runner.user.create(context.Background(), runner.commandEpoch, mustV1UUID(), fmt.Sprintf("V10 platform outage %d", index), "business request during platform outage", v1TraceParent)
		if result.Status == http.StatusCreated {
			success++
			metas = append(metas, result.Meta)
			if primary.CommandID == "" {
				primary = result.Meta
			}
		}
	}
	if err := waitUntil(runner.deadline, windowStarted.Add(v1FaultWindow)); err != nil {
		return runner.failedCase("V10", primaryIdentity(primary, runner.commandEpoch), started, err.Error(), false, nil, int64(time.Since(started)/time.Second), 0), err
	}
	platformUnavailable := runner.admin.stage(context.Background(), primary, true).Status != http.StatusOK
	if err := runner.compose("up", "--detach", "--wait", "--wait-timeout", "420", "platform-api"); err != nil {
		return runner.failedCase("V10", primaryIdentity(primary, runner.commandEpoch), started, "recover platform-api: "+err.Error(), false, nil, int64(time.Since(started)/time.Second), 0), err
	}
	recovered := runner.waitAllStages(metas, time.Minute)
	commandResult := runner.user.commandResult(context.Background(), primary)
	searchStatus, _, searchErr := runner.user.do(context.Background(), http.MethodGet, "/api/v1/search/posts?q="+url.QueryEscape("Phase-24 V10"), nil, nil)
	frontendPostsStatus, _, postsErr := runner.admin.do(context.Background(), http.MethodGet, "/posts", nil, nil)
	frontendAdminStatus, _, adminFrontendErr := runner.admin.do(context.Background(), http.MethodGet, "/admin/", nil, nil)
	noSession := newV1HTTPClient(runner.baseURL)
	unauthenticatedStatus, _, unauthenticatedErr := noSession.do(context.Background(), http.MethodGet, "/api/v1/admin/overview", nil, nil)
	publicInternalStatus, _, publicInternalErr := noSession.do(context.Background(), http.MethodGet, "/internal/v1/business-stages", nil, nil)
	userID, userIDErr := runner.user.currentUserID(context.Background())
	rolePromoteStatus, rolePromoteErr := 0, error(nil)
	rolePromotedAccessStatus := 0
	roleDemoteStatus, roleDemoteErr := 0, error(nil)
	roleDemotedAccessStatus := 0
	if userIDErr == nil {
		rolePromoteStatus, _, rolePromoteErr = runner.admin.do(context.Background(), http.MethodPut, "/api/v1/admin/users/"+strconv.FormatUint(userID, 10)+"/role", nil, map[string]string{"role": "super_admin"})
		if rolePromoteErr == nil && rolePromoteStatus == http.StatusOK {
			rolePromotedAccessStatus, _, rolePromoteErr = runner.user.do(context.Background(), http.MethodGet, "/api/v1/admin/overview", nil, nil)
		}
		roleDemoteStatus, _, roleDemoteErr = runner.admin.do(context.Background(), http.MethodPut, "/api/v1/admin/users/"+strconv.FormatUint(userID, 10)+"/role", nil, map[string]string{"role": "user"})
		if roleDemoteErr == nil && roleDemoteStatus == http.StatusOK {
			roleDemotedAccessStatus, _, roleDemoteErr = runner.user.do(context.Background(), http.MethodGet, "/api/v1/admin/overview", nil, nil)
		}
	}
	internalURL, internalURLErr := runner.internalServiceURL("backend", "8080")
	wrongCredentialStatus := 0
	wrongCredentialErr := internalURLErr
	if wrongCredentialErr == nil {
		internalClient := newV1HTTPClient(internalURL)
		wrongCredentialStatus, _, wrongCredentialErr = internalClient.do(context.Background(), http.MethodGet, "/internal/v1/business-stages", map[string]string{
			"Cookie":                              runner.admin.cookieHeader(),
			"X-GoPulse-Business-Stage-Credential": "wrong-private-credential",
		}, nil)
	}
	facts := v1DurableFacts{}
	var factsErr error
	for _, meta := range metas {
		current, err := runner.facts(meta)
		if err != nil {
			factsErr = errors.Join(factsErr, err)
			continue
		}
		facts.Commands += current.Commands
		facts.Posts += current.Posts
		facts.Outbox += current.Outbox
		facts.Requirements += current.Requirements
		facts.Applications += current.Applications
	}
	item := runner.completedCase("V10", primary, started, "stopped platform-api for sixty seconds, continued ordinary-user writes and reads, checked both same-origin frontend routes, recovered, and verified live role promotion/demotion plus private-route rejection", recovered, recovered, []receipt.RawFact{
		{Name: "requests", Value: v1FaultRequests},
		{Name: "http_success", Value: success},
		{Name: "platform_unavailable", Value: boolInt(platformUnavailable)},
		{Name: "commands", Value: facts.Commands},
		{Name: "posts", Value: facts.Posts},
		{Name: "requirements", Value: facts.Requirements},
		{Name: "applications", Value: facts.Applications},
		{Name: "command_result_available", Value: boolInt(commandResult.Status == http.StatusOK)},
		{Name: "search_available", Value: boolInt(searchStatus == http.StatusOK)},
		{Name: "frontend_routes", Value: boolInt(frontendPostsStatus == http.StatusOK && frontendAdminStatus == http.StatusOK)},
		{Name: "unauthenticated_denied", Value: boolInt(unauthenticatedStatus == http.StatusUnauthorized)},
		{Name: "internal_denied", Value: boolInt(publicInternalStatus != http.StatusOK)},
		{Name: "wrong_private_credential", Value: boolInt(wrongCredentialStatus == http.StatusUnauthorized)},
		{Name: "role_promoted", Value: boolInt(rolePromoteStatus == http.StatusOK && rolePromotedAccessStatus == http.StatusOK)},
		{Name: "role_demoted", Value: boolInt(roleDemoteStatus == http.StatusOK && roleDemotedAccessStatus == http.StatusForbidden)},
	}, 60, int64(time.Since(started)/time.Second))
	for _, failure := range []struct {
		err  error
		name string
	}{
		{searchErr, "search request"}, {postsErr, "frontend posts route"}, {adminFrontendErr, "frontend admin route"}, {unauthenticatedErr, "unauthenticated request"}, {publicInternalErr, "public internal denial"}, {userIDErr, "ordinary user identity"}, {rolePromoteErr, "role promotion"}, {roleDemoteErr, "role demotion"}, {wrongCredentialErr, "private credential request"}, {factsErr, "durable facts"},
	} {
		if failure.err != nil {
			return itemAsFailed(item, "infrastructure_failure", failure.name+": "+failure.err.Error()), failure.err
		}
	}
	if success != v1FaultRequests || facts.Commands != success || facts.Posts != success || facts.Requirements != success || facts.Applications != success || !platformUnavailable || commandResult.Status != http.StatusOK || searchStatus != http.StatusOK || frontendPostsStatus != http.StatusOK || frontendAdminStatus != http.StatusOK || unauthenticatedStatus != http.StatusUnauthorized || publicInternalStatus == http.StatusOK || wrongCredentialStatus != http.StatusUnauthorized || rolePromoteStatus != http.StatusOK || rolePromotedAccessStatus != http.StatusOK || roleDemoteStatus != http.StatusOK || roleDemotedAccessStatus != http.StatusForbidden || !recovered {
		return itemAsFailed(item, "product_failure", "platform-side failure boundary or recovery did not match the contract"), errors.New("V10 platform failure boundary did not hold")
	}
	return item, nil
}

func (runner *formalV1Runner) runV11() (receipt.V1Case, error) {
	started := time.Now().UTC()
	meta, result, err := runner.createPrimary("V11")
	if err != nil {
		return runner.failedCase("V11", identityFromMeta(meta), started, err.Error(), false, nil, 0, 0), err
	}
	stageOK, logsOK := runner.waitStage(meta, true, v1StageWait)
	facts, factsErr := runner.facts(meta)
	oldGeneration, generationErr := runner.searchGeneration()
	if factsErr != nil {
		item := runner.completedCase("V11", result.Meta, started, "created the pre-restore identity", stageOK, logsOK, []receipt.RawFact{{Name: "commands", Value: facts.Commands}}, 0, 0)
		return itemAsFailed(item, "infrastructure_failure", factsErr.Error()), factsErr
	}
	if generationErr != nil {
		item := runner.completedCase("V11", result.Meta, started, "created the pre-restore identity", stageOK, logsOK, []receipt.RawFact{{Name: "commands", Value: facts.Commands}}, 0, 0)
		return itemAsFailed(item, "infrastructure_failure", generationErr.Error()), generationErr
	}
	if runner.bundle == "" {
		item := runner.completedCase("V11", result.Meta, started, "created the pre-restore identity", stageOK, logsOK, []receipt.RawFact{{Name: "commands", Value: facts.Commands}}, 0, 0)
		return itemAsFailed(item, "infrastructure_failure", "candidate bundle path is unavailable"), errors.New("V11 candidate bundle path is unavailable")
	}
	if err := runner.compose("stop"); err != nil {
		item := runner.completedCase("V11", result.Meta, started, "created the pre-restore identity", stageOK, logsOK, []receipt.RawFact{{Name: "commands", Value: facts.Commands}}, 0, 0)
		return itemAsFailed(item, "infrastructure_failure", err.Error()), err
	}
	lifecycleSessionRunner, lifecycleErr := newLifecycleRunner(runner.session, runner.bundle, "clean", "")
	if lifecycleErr != nil {
		_ = runner.compose("up", "--detach", "--wait", "--wait-timeout", "420")
		item := runner.completedCase("V11", result.Meta, started, "created the pre-restore identity", stageOK, logsOK, []receipt.RawFact{{Name: "commands", Value: facts.Commands}}, 0, 0)
		return itemAsFailed(item, "infrastructure_failure", lifecycleErr.Error()), lifecycleErr
	}
	restoreResult, restoreErr := lifecycleSessionRunner.restoreRoundTrip(meta.EventID)
	purgeErr := lifecycleSessionRunner.purge()
	_ = lifecycleSessionRunner.removeOwnedRoot()
	upErr := runner.compose("up", "--detach", "--wait", "--wait-timeout", "420")
	upErr = errors.Join(upErr, purgeErr)
	recoverySeconds := int64(time.Since(started) / time.Second)
	if recoverySeconds < 60 {
		if err := waitUntil(runner.deadline, started.Add(60*time.Second)); err != nil {
			upErr = errors.Join(upErr, err)
		}
		recoverySeconds = 60
	}
	newEpoch := stringStatus(restoreResult, "new_epoch")
	oldEpoch := stringStatus(restoreResult, "expected_old_epoch")
	newGeneration := stringStatus(restoreResult, "restored_generation")
	oldEpochRejected := boolStatus(restoreResult, "old_epoch_rejected")
	oldGenerationNotCompleted := boolStatus(restoreResult, "old_generation_not_completed")
	item := runner.completedCase("V11", result.Meta, started, "stopped the candidate stack, ran the lifecycle drained backup into an empty target, verified old-epoch rejection and new search generation, then recovered the candidate stack", stageOK, logsOK, []receipt.RawFact{
		{Name: "epoch_changed", Value: boolInt(oldEpoch != "" && newEpoch != "" && oldEpoch != newEpoch)},
		{Name: "old_epoch_rejected", Value: boolInt(oldEpochRejected)},
		{Name: "generation_changed", Value: boolInt(newGeneration != "" && newGeneration != oldGeneration)},
		{Name: "old_generation_not_completed", Value: boolInt(oldGenerationNotCompleted)},
		{Name: "restore_verified", Value: boolInt(restoreResult != nil && restoreResult["facts_verified"] == true)},
		{Name: "commands", Value: facts.Commands},
		{Name: "posts", Value: facts.Posts},
	}, 0, recoverySeconds)
	for _, failure := range []struct {
		err  error
		name string
	}{
		{restoreErr, "lifecycle backup/restore"}, {upErr, "candidate stack recovery"},
	} {
		if failure.err != nil {
			return itemAsFailed(item, "infrastructure_failure", failure.name+": "+failure.err.Error()), failure.err
		}
	}
	if restoreResult == nil || restoreResult["facts_verified"] != true || !oldEpochRejected || !oldGenerationNotCompleted || oldEpoch == "" || newEpoch == "" || oldEpoch == newEpoch || newGeneration == "" || newGeneration == oldGeneration || recoverySeconds < 60 || !stageOK {
		return itemAsFailed(item, "product_failure", "restore did not establish a new epoch and generation with stale completion fencing"), errors.New("V11 epoch and recovery invariant did not hold")
	}
	return item, nil
}

func (runner *formalV1Runner) runV12() (receipt.V1Case, error) {
	started := time.Now().UTC()
	meta, result, err := runner.createPrimary("V12")
	if err != nil {
		return runner.failedCase("V12", identityFromMeta(meta), started, err.Error(), false, nil, 0, 0), err
	}
	taskID := mustV1UUID()
	childPath := filepath.Join(runner.rawDirectory, "children", "V12.json")
	ownedResource := filepath.Join(runner.rawDirectory, "children", "owned-"+taskID+".tmp")
	if err := os.MkdirAll(filepath.Dir(ownedResource), 0o700); err != nil {
		return runner.failedCase("V12", identityFromMeta(result.Meta), started, err.Error(), false, nil, 0, 0), err
	}
	if err := os.WriteFile(ownedResource, []byte("V12 child-owned resource\n"), 0o600); err != nil {
		return runner.failedCase("V12", identityFromMeta(result.Meta), started, err.Error(), false, nil, 0, 0), err
	}
	childStarted := make(chan struct{})
	childActive := make(chan struct{})
	cancelRequested := make(chan time.Time, 1)
	childDone := make(chan error, 1)
	childContext, cancel := context.WithCancel(context.Background())
	go func() {
		childStart := time.Now().UTC()
		stages := []string{"started"}
		close(childStarted)
		finishChild := func(requested time.Time) error {
			cleanupRemoved := os.Remove(ownedResource) == nil
			if !cleanupRemoved {
				if _, statErr := os.Stat(ownedResource); os.IsNotExist(statErr) {
					cleanupRemoved = true
				}
			}
			stages = appendUniqueString(stages, "cancelled")
			child := receipt.ChildReceipt{CaseID: "V12", TaskID: taskID, Status: "canceled", Identity: identityFromMeta(result.Meta), StartedAt: childStart.Format(time.RFC3339Nano), CancelRequestedAt: requested.Format(time.RFC3339Nano), FinishedAt: time.Now().UTC().Format(time.RFC3339Nano), CompletedStages: stages, CleanupRemoved: cleanupRemoved, OwnedResource: "v1-v12-child-" + taskID}
			if writeErr := receipt.WriteChildReceipt(childPath, child); writeErr != nil {
				return writeErr
			}
			checksum, hashErr := receipt.DigestFile(childPath)
			if hashErr == nil {
				runner.children["V12"] = receipt.RawRecord{Path: "children/V12.json", SHA256: checksum}
			}
			return hashErr
		}
		active := false
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			runner.admin.stage(childContext, result.Meta, false)
			stages = appendUniqueString(stages, "stage_poll")
			select {
			case requested := <-cancelRequested:
				childDone <- finishChild(requested)
				return
			default:
			}
			_, _ = runner.facts(result.Meta)
			stages = appendUniqueString(stages, "durable_fact_poll")
			if !active {
				close(childActive)
				active = true
			}
			select {
			case requested := <-cancelRequested:
				childDone <- finishChild(requested)
				return
			case <-ticker.C:
			}
		}
	}()
	<-childStarted
	<-childActive
	time.Sleep(250 * time.Millisecond)
	requested := time.Now().UTC()
	cancelRequested <- requested
	cancel()
	childErr := <-childDone
	stageOK, logsOK := runner.waitStage(meta, true, v1StageWait)
	facts, factsErr := runner.facts(meta)
	item := runner.completedCase("V12", result.Meta, started, "cancelled a real child task running repeated stage and durable-fact work, wrote a canceled child receipt, and removed its owned resource before parent recovery", stageOK, logsOK, []receipt.RawFact{{Name: "cancelled", Value: boolInt(childErr == nil)}, {Name: "child_incomplete", Value: boolInt(childErr == nil)}, {Name: "child_cleanup_removed", Value: boolInt(childErr == nil)}, {Name: "commands", Value: facts.Commands}, {Name: "posts", Value: facts.Posts}, {Name: "cleanup_removed", Value: boolInt(childErr == nil)}}, 0, 0)
	if childErr != nil {
		return itemAsFailed(item, "infrastructure_failure", "write V12 child receipt: "+childErr.Error()), childErr
	}
	if factsErr != nil {
		return itemAsFailed(item, "infrastructure_failure", factsErr.Error()), factsErr
	}
	if _, err := os.Stat(ownedResource); !os.IsNotExist(err) {
		return itemAsFailed(item, "product_failure", "V12 child-owned resource was not removed"), errors.New("V12 child cleanup did not remove the owned resource")
	}
	if !stageOK || facts.Commands != 1 || facts.Posts != 1 {
		return itemAsFailed(item, "product_failure", "parent identity did not recover after child cancellation"), errors.New("V12 parent recovery invariant did not hold")
	}
	return item, nil
}

func appendUniqueString(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func (runner *formalV1Runner) compose(args ...string) error {
	result := runner.session.Compose(args...)
	if result.ExitCode != 0 {
		return fmt.Errorf("Compose %s failed with exit code %d", strings.Join(args, " "), result.ExitCode)
	}
	return nil
}

func (runner *formalV1Runner) startFaultProxy(mode string) (string, error) {
	runner.fixtureOnce.Do(func() {
		path := filepath.Join(runner.session.TempDir, "business-fault-proxy")
		// The repository is intentionally multi-module: the acceptance module owns
		// this fixture, while the repository root has no go.mod.  Use Go's -C
		// option so the command is still recorded by the session but resolves the
		// fixture from acceptance/go.mod rather than failing at the repository root.
		result := runner.session.Run("go", "-C", "acceptance", "build", "-trimpath", "-o", path, "./internal/fixtures/business-fault-proxy")
		if result.ExitCode != 0 {
			detail := strings.TrimSpace(result.Stderr)
			if detail == "" {
				runner.fixtureErr = fmt.Errorf("build business fault fixture failed with exit code %d", result.ExitCode)
				return
			}
			runner.fixtureErr = fmt.Errorf("build business fault fixture failed with exit code %d: %s", result.ExitCode, detail)
			return
		}
		runner.fixtureBinary = path
	})
	if runner.fixtureErr != nil {
		return "", runner.fixtureErr
	}
	port, err := v1FreePort()
	if err != nil {
		return "", err
	}
	command := exec.Command(runner.fixtureBinary, "--listen", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), "--upstream", runner.baseURL, "--mode", mode)
	command.Dir = runner.session.Root
	command.Env = runner.session.Environment()
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return "", err
	}
	endpoint := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		connection, dialErr := net.DialTimeout("tcp", endpoint, 100*time.Millisecond)
		if dialErr == nil {
			_ = connection.Close()
			runner.fixtureProcess = command
			return "http://" + endpoint, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = command.Process.Kill()
	_ = command.Wait()
	return "", errors.New("business fault fixture did not start")
}

func (runner *formalV1Runner) stopFaultProxy() error {
	if runner.fixtureProcess == nil {
		return nil
	}
	process := runner.fixtureProcess
	runner.fixtureProcess = nil
	if err := process.Process.Signal(os.Interrupt); err != nil {
		_ = process.Process.Kill()
	}
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		return nil
	case <-time.After(5 * time.Second):
		_ = process.Process.Kill()
		return <-done
	}
}

func (runner *formalV1Runner) completedCase(caseID string, meta v1HTTPMeta, started time.Time, action string, stageOK, logsOK bool, facts []receipt.RawFact, window, recovery int64) receipt.V1Case {
	if meta.PostID == 0 || meta.CommandID == "" || meta.EventID == "" {
		return notStartedCase(caseID)
	}
	return runner.writeCase(caseID, meta, started, "passed", "", action, stageOK, logsOK, facts, window, recovery)
}

func (runner *formalV1Runner) failedCase(caseID string, identity receipt.V1Identity, started time.Time, reason string, stageOK bool, facts []receipt.RawFact, window, recovery int64) receipt.V1Case {
	if identity.PostID == 0 || identity.CommandID == "" || identity.EventID == "" {
		return notStartedCase(caseID)
	}
	return runner.writeCase(caseID, v1HTTPMeta{CommandID: identity.CommandID, EventID: identity.EventID, DataEpoch: identity.DataEpoch, PostID: identity.PostID}, started, "failed", "infrastructure_failure", reason, stageOK, stageOK, facts, window, recovery)
}

func (runner *formalV1Runner) writeCase(caseID string, meta v1HTTPMeta, started time.Time, status, failureClass, action string, stageOK, logsOK bool, facts []receipt.RawFact, window, recovery int64) receipt.V1Case {
	identity := identityFromMeta(meta)
	if identity.DataEpoch == "" {
		identity.DataEpoch = runner.commandEpoch
	}
	if identity.CommandID == "" || identity.EventID == "" || identity.PostID == 0 {
		// A case without a real committed identity has not started. Never make
		// up IDs merely to satisfy the raw-evidence schema.
		return notStartedCase(caseID)
	}
	rawPath := filepath.Join(runner.rawDirectory, "cases", caseID+".json")
	checks := receipt.RawChecks{Created: meta.PostID > 0, StagePage: stageOK, PreciseLogsLink: logsOK}
	if err := receipt.WriteRawEvidence(rawPath, receipt.RawEvidence{CaseID: caseID, Identity: identity, Checks: checks, CapturedAt: time.Now().UTC().Format(time.RFC3339Nano), Facts: facts}); err != nil {
		status = "failed"
		failureClass = "infrastructure_failure"
		action += "; raw evidence write failed: " + err.Error()
	}
	checksum, err := receipt.DigestFile(rawPath)
	if err != nil {
		checksum = "sha256:" + strings.Repeat("0", 64)
		status = "failed"
		failureClass = "infrastructure_failure"
		action += "; raw evidence hash failed: " + err.Error()
	}
	var child *receipt.RawRecord
	if record, ok := runner.children[caseID]; ok {
		child = &record
	}
	return receipt.V1Case{CaseID: caseID, Status: status, FailureClass: failureClass, PlannedAction: v1PlannedActions[caseID], ActualAction: action, Identity: identity, Moments: []receipt.Moment{{Name: "action_started", Actual: started.UTC().Format(time.RFC3339Nano)}, {Name: "case_finished", Actual: time.Now().UTC().Format(time.RFC3339Nano)}}, WindowSeconds: window, RecoverySeconds: recovery, Raw: receipt.RawRecord{Path: "cases/" + caseID + ".json", SHA256: checksum}, Child: child, CleanupPassed: true}
}

func itemAsFailed(item receipt.V1Case, failureClass, reason string) receipt.V1Case {
	if item.Status == "not_started" {
		return item
	}
	item.Status = "failed"
	item.FailureClass = failureClass
	item.ActualAction = strings.TrimSpace(item.ActualAction + "; " + reason)
	return item
}

func notStartedCase(caseID string) receipt.V1Case {
	return receipt.V1Case{CaseID: caseID, Status: "not_started", PlannedAction: v1PlannedActions[caseID]}
}

func replaceV1Case(evidence *receipt.V1Evidence, item receipt.V1Case) {
	for index := range evidence.Cases {
		if evidence.Cases[index].CaseID == item.CaseID {
			evidence.Cases[index] = item
			return
		}
	}
}

func identityFromMeta(meta v1HTTPMeta) receipt.V1Identity {
	return receipt.V1Identity{DataEpoch: meta.DataEpoch, CommandID: meta.CommandID, EventID: meta.EventID, PostID: meta.PostID}
}

func primaryIdentity(meta v1HTTPMeta, epoch string) receipt.V1Identity {
	identity := identityFromMeta(meta)
	if identity.DataEpoch == "" {
		identity.DataEpoch = epoch
	}
	return identity
}

func firstSuccessful(results []v1CreateResult) (v1CreateResult, bool) {
	for _, result := range results {
		if result.Status == http.StatusCreated && result.Meta.CommandID != "" {
			return result, true
		}
	}
	return v1CreateResult{}, false
}

func countStatus(results []v1CreateResult, status int) int {
	count := 0
	for _, result := range results {
		if result.Status == status {
			count++
		}
	}
	return count
}

func boolInt(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func boolStatus(value map[string]any, name string) bool {
	if value == nil {
		return false
	}
	result, ok := value[name].(bool)
	return ok && result
}

func sqlLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func shellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func mustV1UUID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], raw[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], raw[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], raw[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], raw[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], raw[10:16])
	return string(encoded)
}

func v1FreePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func waitUntil(deadline, target time.Time) error {
	if target.After(deadline) {
		return errors.New("V1 budget exhausted before scheduled fault action")
	}
	if delay := time.Until(target); delay > 0 {
		time.Sleep(delay)
	}
	return nil
}
