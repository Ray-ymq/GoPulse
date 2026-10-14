package control

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"
)

const (
	composeDiagnosticLimit = 128 << 10
	serviceLogLimit        = 64 << 10
	serviceLogTail         = 200
)

var (
	diagnosticQuotedSecret = regexp.MustCompile(`(?i)((?:password|passwd|token|secret|credential|authorization|cookie|api[_-]?key)\b["']?\s*[:=]\s*)"[^"]*"`)
	diagnosticSingleSecret = regexp.MustCompile(`(?i)((?:password|passwd|token|secret|credential|authorization|cookie|api[_-]?key)\b["']?\s*[:=]\s*)'[^']*'`)
	diagnosticBareSecret   = regexp.MustCompile(`(?i)((?:password|passwd|token|secret|credential|authorization|cookie|api[_-]?key)\b["']?\s*[:=]\s*)[^\s,;}]+`)
	diagnosticURLSecret    = regexp.MustCompile(`(?i)(://[^/\s:@]+:)[^@\s/]+@`)
)

type composeFailure struct {
	Action       string `json:"action"`
	ExitCode     int    `json:"exit_code"`
	ProcessError string `json:"process_error,omitempty"`
	Stdout       string `json:"stdout,omitempty"`
	Stderr       string `json:"stderr,omitempty"`
	Truncated    bool   `json:"truncated,omitempty"`
}

type boundedDiagnosticOutput struct {
	bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedDiagnosticOutput) Write(p []byte) (int, error) {
	limit := b.limit
	if limit <= 0 {
		limit = composeDiagnosticLimit
	}
	remaining := limit - b.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = b.Buffer.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

type diagnosticServiceStatus struct {
	State    string `json:"state"`
	Health   string `json:"health,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
}

type lifecycleFailureDiagnostic struct {
	Schema          int                                `json:"schema"`
	CapturedAt      string                             `json:"captured_at"`
	Command         string                             `json:"command"`
	OperationID     string                             `json:"operation_id"`
	Phase           string                             `json:"phase"`
	FailureStage    string                             `json:"failure_stage"`
	ExitCode        int                                `json:"exit_code"`
	Error           string                             `json:"error"`
	Compose         *composeFailure                    `json:"compose,omitempty"`
	Services        map[string]diagnosticServiceStatus `json:"services"`
	Logs            map[string]string                  `json:"logs,omitempty"`
	CollectionError map[string]string                  `json:"collection_errors,omitempty"`
}

// redactDiagnostic applies a second, shape-based pass after known lifecycle
// values have been replaced. Diagnostics contain only bounded Compose output
// and service logs, so this protects against a log line that prints a secret
// under a different generated value or with a common credential key.
func (c *Controller) redactDiagnostic(text string) string {
	text = diagnosticQuotedSecret.ReplaceAllString(text, `${1}"[REDACTED]"`)
	text = diagnosticSingleSecret.ReplaceAllString(text, `${1}'[REDACTED]'`)
	text = diagnosticBareSecret.ReplaceAllString(text, `${1}[REDACTED]`)
	return diagnosticURLSecret.ReplaceAllString(text, `${1}[REDACTED]@`)
}

func commandExitCode(err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return 1
}

func (c *Controller) captureFailureDiagnostic(err error, command string) error {
	var failure *Failure
	if !errors.As(err, &failure) {
		failure = &Failure{Code: Failed, Stage: c.state.Phase, Message: "lifecycle operation failed"}
	}
	if c.failureDiagnosticSaved {
		return failure
	}
	// A lock can fail before the controller loads the installation state. Do
	// not serialize the zero-value controller over a valid state file while
	// capturing diagnostics for that pre-load failure.
	if c.state.Schema == 1 && c.state.Project != "" && len(c.state.Token) == 64 && c.state.FailedStage == "" {
		c.state.FailedStage = failure.Stage
		_ = c.save()
	}
	if c.manifest == nil || c.state.Project == "" || len(c.state.Token) != 64 || privateDir(c.dir) != nil {
		return failure
	}
	directory := filepath.Join(c.dir, "diagnostics")
	if err := os.MkdirAll(directory, 0700); err != nil || privateDir(directory) != nil {
		return failure
	}
	operation := c.state.Operation
	if !validOperationID(operation) {
		operation = random()
	}
	services, references, collectionErrors := c.diagnosticServices()
	logs := c.diagnosticLogs(references, collectionErrors)
	diagnostic := lifecycleFailureDiagnostic{
		Schema:          1,
		CapturedAt:      time.Now().UTC().Format(time.RFC3339Nano),
		Command:         command,
		OperationID:     operation,
		Phase:           c.state.Phase,
		FailureStage:    failure.Stage,
		ExitCode:        failure.Code,
		Error:           c.redactDiagnostic(failure.Message),
		Compose:         c.lastComposeFailure,
		Services:        services,
		Logs:            logs,
		CollectionError: collectionErrors,
	}
	if diagnostic.Compose != nil {
		diagnostic.Compose.ProcessError = c.redact(diagnostic.Compose.ProcessError)
		diagnostic.Compose.Stdout = c.redact(diagnostic.Compose.Stdout)
		diagnostic.Compose.Stderr = c.redact(diagnostic.Compose.Stderr)
	}
	path := filepath.Join(directory, "failure-"+operation+".json")
	if atomicFile(path, marshal(diagnostic)) == nil {
		c.failureDiagnosticSaved = true
		failure.Diagnostic = path
	}
	return failure
}

func (c *Controller) diagnosticServices() (map[string]diagnosticServiceStatus, map[string][]string, map[string]string) {
	statuses := map[string]diagnosticServiceStatus{}
	references := map[string][]string{}
	errorsByScope := map[string]string{}
	for _, service := range c.diagnosticServiceNames() {
		statuses[service] = diagnosticServiceStatus{State: "missing"}
	}
	containers, err := c.containers()
	if err != nil {
		errorsByScope["services"] = "service status unavailable"
		return statuses, references, errorsByScope
	}
	for _, container := range containers {
		labels := container.Config.Labels
		if labels["com.docker.compose.project"] != c.state.Project || labels[prefix+"installation"] != c.state.Token || labels[prefix+"manifest"] != c.manifestHash {
			continue
		}
		service := labels["com.docker.compose.service"]
		if !c.diagnosticServiceAllowed(service) {
			continue
		}
		state := diagnosticServiceStatus{State: container.State.Status, ExitCode: container.State.ExitCode}
		if container.State.Health != nil {
			state.Health = container.State.Health.Status
		}
		if current, ok := statuses[service]; ok && current.State != "missing" {
			statuses[service] = diagnosticServiceStatus{State: "duplicate"}
			continue
		}
		statuses[service] = state
		references[service] = append(references[service], container.ID)
	}
	return statuses, references, errorsByScope
}

func (c *Controller) diagnosticServiceNames() []string {
	services := make([]string, 0, len(c.services))
	for name, raw := range c.services {
		if name == "lifecycle" || !c.diagnosticServiceAllowed(name) {
			continue
		}
		service, ok := raw.(map[string]any)
		if !ok || service["profiles"] != nil {
			continue
		}
		services = append(services, name)
	}
	sort.Strings(services)
	return services
}

func (c *Controller) diagnosticServiceAllowed(service string) bool {
	_, ok := c.services[service]
	return ok && service != "" && service != "lifecycle"
}

func (c *Controller) diagnosticLogs(references map[string][]string, errorsByScope map[string]string) map[string]string {
	logs := map[string]string{}
	services := make([]string, 0, len(references))
	for service := range references {
		services = append(services, service)
	}
	sort.Strings(services)
	for _, service := range services {
		combined := boundedDiagnosticOutput{limit: serviceLogLimit}
		for _, id := range references[service] {
			cmd := exec.CommandContext(c.ctx, "docker", "--host", c.endpoint, "logs", "--tail", strconv.Itoa(serviceLogTail), "--timestamps", id)
			isolate(cmd)
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=/tmp", "DOCKER_CONFIG=/tmp/gopulse-docker-empty"}
			output := boundedDiagnosticOutput{limit: serviceLogLimit}
			cmd.Stdout = &output
			cmd.Stderr = &output
			if err := cmd.Run(); err != nil {
				errorsByScope["logs:"+service] = "service logs unavailable"
				continue
			}
			if combined.Len() > 0 {
				_, _ = combined.Write([]byte{'\n'})
			}
			_, _ = combined.Write([]byte(output.String()))
			if output.truncated || combined.truncated {
				_, _ = combined.Write([]byte("\n[diagnostic log truncated]\n"))
			}
		}
		if combined.Len() > 0 {
			logs[service] = c.redact(combined.String())
		}
	}
	return logs
}
