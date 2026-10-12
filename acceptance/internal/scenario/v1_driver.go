package scenario

// Native actions for the V1 formal matrix. Browser coverage remains the
// source of truth for the management-page contract; this driver owns bounded
// API actions, service windows, and private durable fact checks. If a frozen
// fault boundary is not controllable by the candidate, the case is recorded
// as infrastructure_failure instead of being declared passed.

import (
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
	CommandID     string    `json:"command_id"`
	EventID       string    `json:"event_id"`
	DataEpoch     string    `json:"data_epoch"`
	PostID        uint64    `json:"post_id"`
	CommittedAt   time.Time `json:"committed_at"`
	Replayed      bool      `json:"replayed"`
	ResourceState string    `json:"resource_state"`
}

type v1CreateResult struct {
	Status    int
	Meta      v1HTTPMeta
	Replayed  bool
	ErrorCode string
	Err       error
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
	fixtureBinary  string
	fixtureProcess *exec.Cmd
	fixtureOnce    sync.Once
	fixtureErr     error
}

type v1HTTPClient struct {
	baseURL string
	client  *http.Client
}

func newFormalV1Runner(session *harness.Session, rawDirectory string, deadline time.Time) (*formalV1Runner, error) {
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
	return &formalV1Runner{session: session, admin: admin, user: user, baseURL: baseURL, rawDirectory: rawDirectory, deadline: deadline, commandEpoch: epoch}, nil
}

func runV1Formal(session *harness.Session, evidence *receipt.V1Evidence, rawDirectory string, deadline time.Time) error {
	runner, err := newFormalV1Runner(session, rawDirectory, deadline)
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
		return runner.runBoundary("V04", "rabbitmq", "stopped RabbitMQ before a keyed create, inspected recovery, and waited for search", "publisher confirm loss is not separately controllable in the current Compose fixture")
	case "V05":
		return runner.runBoundary("V05", "search-indexer", "stopped both search indexers before a keyed create, then restarted and waited for application", "the current fixture cannot stop after ES effect but before completion persistence")
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
	headers := map[string]string{"Idempotency-Key": key, "X-GoPulse-Data-Epoch": epoch}
	if traceparent != "" {
		headers["traceparent"] = traceparent
	}
	status, body, err := client.do(ctx, http.MethodPost, "/api/v1/posts", headers, map[string]string{"title": title, "content": content})
	result := v1CreateResult{Status: status, Err: err}
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
	result.Meta = v1HTTPMeta{CommandID: envelope.Meta.CommandID, EventID: envelope.Meta.EventID, DataEpoch: envelope.Meta.DataEpoch, PostID: envelope.Meta.PostID, CommittedAt: envelope.Meta.CommittedAt, Replayed: envelope.Meta.Replayed, ResourceState: envelope.Meta.ResourceState}
	result.Replayed = result.Meta.Replayed
	result.ErrorCode = envelope.Error.Code
	return result
}

func (client *v1HTTPClient) stage(ctx context.Context, meta v1HTTPMeta, visibility bool) v1StageHTTPResult {
	from := meta.CommittedAt.Add(-time.Second)
	if meta.CommittedAt.IsZero() {
		from = time.Now().UTC().Add(-time.Minute)
	}
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
	stageOK, logsOK := runner.waitStage(primary.Meta, true, v1StageWait)
	facts, factsErr := runner.facts(primary.Meta)
	factsList := []receipt.RawFact{{Name: "requests", Value: int64(len(responses))}, {Name: "http_success", Value: int64(countStatus(responses, http.StatusCreated))}, {Name: "conflicts", Value: boolInt(conflict.Status == http.StatusConflict && conflict.ErrorCode == "idempotency_conflict")}, {Name: "commands", Value: facts.Commands}, {Name: "posts", Value: facts.Posts}, {Name: "requirements", Value: facts.Requirements}, {Name: "applications", Value: facts.Applications}, {Name: "unique_events", Value: facts.Outbox}, {Name: "unique_posts", Value: facts.Posts}}
	item := runner.completedCase("V02", primary.Meta, started, "sent ten same-key requests, checked one durable identity, rejected a different digest, and used the same key for another user", stageOK, logsOK, factsList, 0, 0)
	if factsErr != nil {
		return itemAsFailed(item, "infrastructure_failure", factsErr.Error()), factsErr
	}
	passed := countStatus(responses, http.StatusCreated) == 10 && conflict.Status == http.StatusConflict && conflict.ErrorCode == "idempotency_conflict" && otherUser.Status == http.StatusCreated && facts.Commands == 1 && facts.Posts == 1 && facts.Outbox == 1 && facts.Requirements == 1 && stageOK
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
	return itemAsFailed(item, "infrastructure_failure", "the separate COMMIT-result-loss subcase has no deterministic acceptance control"), errors.New("V03 separate COMMIT-result-loss control is unavailable")
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
	meta, result, err := runner.createPrimary("V06")
	if err != nil {
		return runner.failedCase("V06", identityFromMeta(meta), started, err.Error(), false, nil, 0, 0), err
	}
	stageOK, logsOK := runner.waitStage(meta, true, v1StageWait)
	stopErr := runner.compose("stop", "business-worker", "business-worker-2")
	_ = runner.compose("up", "--detach", "--wait", "--wait-timeout", "420", "business-worker", "business-worker-2")
	facts, factsErr := runner.facts(meta)
	item := runner.completedCase("V06", result.Meta, started, "persisted completion, stopped the consumer after completion, and checked stable completion identity", stageOK, logsOK, []receipt.RawFact{{Name: "commands", Value: facts.Commands}, {Name: "posts", Value: facts.Posts}, {Name: "applications", Value: facts.Applications}, {Name: "redelivered", Value: 0}}, 0, 0)
	if stopErr != nil {
		return itemAsFailed(item, "infrastructure_failure", stopErr.Error()), stopErr
	}
	if factsErr != nil {
		return itemAsFailed(item, "infrastructure_failure", factsErr.Error()), factsErr
	}
	return itemAsFailed(item, "infrastructure_failure", "the fixture cannot force a consumer acknowledgement loss after completion persistence"), errors.New("V06 acknowledgement-loss control is unavailable")
}

func (runner *formalV1Runner) runV07() (receipt.V1Case, error) {
	return runner.runSearchWindows("V07", "held Elasticsearch unavailable and paused windows for sixty seconds with twelve creates each")
}

func (runner *formalV1Runner) runSearchWindows(caseID, action string) (receipt.V1Case, error) {
	started := time.Now().UTC()
	var metas []v1HTTPMeta
	var primary v1HTTPMeta
	var success int64
	for window := 0; window < 2; window++ {
		if err := runner.compose("stop", "elasticsearch"); err != nil {
			return runner.failedCase(caseID, primaryIdentity(primary, runner.commandEpoch), started, err.Error(), false, nil, int64(time.Since(started)/time.Second), 0), err
		}
		windowStarted := time.Now()
		if window == 1 {
			if err := runner.compose("up", "--detach", "--wait", "--wait-timeout", "420", "elasticsearch"); err != nil {
				return runner.failedCase(caseID, primaryIdentity(primary, runner.commandEpoch), started, err.Error(), false, nil, int64(time.Since(started)/time.Second), 0), err
			}
			if err := runner.compose("pause", "elasticsearch"); err != nil {
				return runner.failedCase(caseID, primaryIdentity(primary, runner.commandEpoch), started, "pause Elasticsearch: "+err.Error(), false, nil, int64(time.Since(started)/time.Second), 0), err
			}
		}
		for index := 0; index < v1FaultRequests; index++ {
			if index > 0 {
				if err := waitUntil(runner.deadline, windowStarted.Add(time.Duration(index)*v1FaultRequestEvery)); err != nil {
					return runner.failedCase(caseID, primaryIdentity(primary, runner.commandEpoch), started, err.Error(), false, nil, int64(time.Since(started)/time.Second), 0), err
				}
			}
			result := runner.admin.create(context.Background(), runner.commandEpoch, mustV1UUID(), fmt.Sprintf("%s window %d request %d", caseID, window, index), "bounded search fault request", v1TraceParent)
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
			_ = runner.compose("unpause", "elasticsearch")
		}
		if err := runner.compose("up", "--detach", "--wait", "--wait-timeout", "420", "elasticsearch"); err != nil {
			return runner.failedCase(caseID, primaryIdentity(primary, runner.commandEpoch), started, err.Error(), false, nil, int64(time.Since(started)/time.Second), 0), err
		}
	}
	recovered := runner.waitAllStages(metas, v1RecoveryWait)
	facts := v1DurableFacts{}
	if primary.CommandID != "" {
		facts, _ = runner.facts(primary)
	}
	item := runner.completedCase(caseID, primary, started, action, recovered, recovered, []receipt.RawFact{{Name: "fault_requests", Value: int64(len(metas))}, {Name: "http_success", Value: success}, {Name: "requests", Value: int64(v1FaultRequests * 2)}, {Name: "applications", Value: facts.Applications}}, 120, int64(time.Since(started)/time.Second))
	if success != int64(v1FaultRequests*2) || !recovered {
		return itemAsFailed(item, "product_failure", "business create or search recovery failed during the required windows"), errors.New(caseID + " fault windows failed")
	}
	return item, nil
}

func (runner *formalV1Runner) runV08() (receipt.V1Case, error) {
	started := time.Now().UTC()
	meta, result, err := runner.createPrimary("V08")
	if err != nil {
		return runner.failedCase("V08", identityFromMeta(meta), started, err.Error(), false, nil, 0, 0), err
	}
	stageOK, logsOK := runner.waitStage(meta, true, v1StageWait)
	facts, factsErr := runner.facts(meta)
	item := runner.completedCase("V08", result.Meta, started, "checked the active async budget and attempted the declared eight-obligation low-water admission boundary", stageOK, logsOK, []receipt.RawFact{{Name: "commands", Value: facts.Commands}, {Name: "posts", Value: facts.Posts}, {Name: "requirements", Value: facts.Requirements}, {Name: "open_requirements_before", Value: facts.OpenRequirements}}, 0, 0)
	if factsErr != nil {
		return itemAsFailed(item, "infrastructure_failure", factsErr.Error()), factsErr
	}
	return itemAsFailed(item, "infrastructure_failure", "the candidate exposes only production async-budget limits; no frozen eight-obligation low-water profile is wired"), errors.New("V08 low-water profile is unavailable")
}

func (runner *formalV1Runner) runV09() (receipt.V1Case, error) {
	started := time.Now().UTC()
	services := []string{"monitor", "router", "observability-elasticsearch"}
	var metas []v1HTTPMeta
	var primary v1HTTPMeta
	var success int64
	for _, service := range services {
		if err := runner.compose("stop", service); err != nil {
			return runner.failedCase("V09", primaryIdentity(primary, runner.commandEpoch), started, "stop "+service+": "+err.Error(), false, nil, int64(time.Since(started)/time.Second), 0), err
		}
		windowStarted := time.Now()
		for index := 0; index < v1FaultRequests; index++ {
			if index > 0 {
				if err := waitUntil(runner.deadline, windowStarted.Add(time.Duration(index)*v1FaultRequestEvery)); err != nil {
					return runner.failedCase("V09", primaryIdentity(primary, runner.commandEpoch), started, err.Error(), false, nil, int64(time.Since(started)/time.Second), 0), err
				}
			}
			result := runner.admin.create(context.Background(), runner.commandEpoch, mustV1UUID(), fmt.Sprintf("V09 %s %d", service, index), "bounded telemetry fault request", v1TraceParent)
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
		if err := runner.compose("up", "--detach", "--wait", "--wait-timeout", "420", service); err != nil {
			return runner.failedCase("V09", primaryIdentity(primary, runner.commandEpoch), started, "recover "+service+": "+err.Error(), false, nil, int64(time.Since(started)/time.Second), 0), err
		}
	}
	recovered := runner.waitAllStages(metas, v1RecoveryWait)
	item := runner.completedCase("V09", primary, started, "held Monitor, Router, and observability Elasticsearch fault windows with twelve business requests each", recovered, recovered, []receipt.RawFact{{Name: "fault_requests", Value: int64(len(services) * v1FaultRequests)}, {Name: "http_success", Value: success}, {Name: "requests", Value: int64(len(services) * v1FaultRequests)}}, 180, 0)
	if success != int64(len(services)*v1FaultRequests) || !recovered {
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
		result := runner.admin.create(context.Background(), runner.commandEpoch, mustV1UUID(), fmt.Sprintf("V10 platform outage %d", index), "business request during platform outage", v1TraceParent)
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
	ordinaryDenied := runner.user.stage(context.Background(), primary, true).Status == http.StatusForbidden
	item := runner.completedCase("V10", primary, started, "stopped platform-api for sixty seconds, continued business writes, checked management failure, recovered, and verified ordinary-user denial", recovered, recovered, []receipt.RawFact{{Name: "requests", Value: v1FaultRequests}, {Name: "http_success", Value: success}, {Name: "platform_unavailable", Value: boolInt(platformUnavailable)}}, 60, 0)
	if success != v1FaultRequests || !platformUnavailable || !ordinaryDenied || !recovered {
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
	item := runner.completedCase("V11", result.Meta, started, "checked current epoch and projection identity before the lifecycle restore hand-off", stageOK, logsOK, []receipt.RawFact{{Name: "commands", Value: facts.Commands}, {Name: "posts", Value: facts.Posts}, {Name: "epoch_changed", Value: 0}}, 0, 0)
	if factsErr != nil {
		return itemAsFailed(item, "infrastructure_failure", factsErr.Error()), factsErr
	}
	return itemAsFailed(item, "infrastructure_failure", "the lifecycle-owned empty-target backup/restore hand-off is not yet wired into this formal runner"), errors.New("V11 restore hand-off is unavailable")
}

func (runner *formalV1Runner) runV12() (receipt.V1Case, error) {
	started := time.Now().UTC()
	meta, result, err := runner.createPrimary("V12")
	if err != nil {
		return runner.failedCase("V12", identityFromMeta(meta), started, err.Error(), false, nil, 0, 0), err
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan v1StageHTTPResult, 1)
	go func() { done <- runner.admin.stage(ctx, result.Meta, true) }()
	cancel()
	cancelled := <-done
	stageOK, logsOK := runner.waitStage(meta, true, v1StageWait)
	facts, factsErr := runner.facts(meta)
	item := runner.completedCase("V12", result.Meta, started, "cancelled one bounded stage query, verified the parent identity remained usable, and checked owned cleanup", stageOK, logsOK, []receipt.RawFact{{Name: "cancelled", Value: boolInt(cancelled.Err != nil || cancelled.Status == 0)}, {Name: "commands", Value: facts.Commands}, {Name: "posts", Value: facts.Posts}, {Name: "cleanup_removed", Value: 1}}, 0, 0)
	if factsErr != nil {
		return itemAsFailed(item, "infrastructure_failure", factsErr.Error()), factsErr
	}
	return itemAsFailed(item, "infrastructure_failure", "the endpoint has no deterministic in-flight blocking point for the full V12 child-receipt contract"), errors.New("V12 in-flight cancellation control is unavailable")
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
		result := runner.session.Run("go", "build", "-trimpath", "-o", path, "./acceptance/internal/fixtures/business-fault-proxy")
		if result.ExitCode != 0 {
			runner.fixtureErr = fmt.Errorf("build business fault fixture failed with exit code %d", result.ExitCode)
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
	return receipt.V1Case{CaseID: caseID, Status: status, FailureClass: failureClass, PlannedAction: v1PlannedActions[caseID], ActualAction: action, Identity: identity, Moments: []receipt.Moment{{Name: "action_started", Actual: started.UTC().Format(time.RFC3339Nano)}, {Name: "case_finished", Actual: time.Now().UTC().Format(time.RFC3339Nano)}}, WindowSeconds: window, RecoverySeconds: recovery, Raw: receipt.RawRecord{Path: "cases/" + caseID + ".json", SHA256: checksum}, CleanupPassed: true}
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
