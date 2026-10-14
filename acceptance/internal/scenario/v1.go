package scenario

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/acceptance/internal/harness"
	"github.com/Ray-ymq/GoPulse/acceptance/internal/receipt"
)

var v1PlannedActions = map[string]string{
	"V01": "create post and observe committed-to-applied stage page",
	"V02": "repeat the same idempotency key and compare one command/event identity",
	"V03": "drop the HTTP response after durable commit and retry the same command",
	"V04": "drop the broker confirmation after publish and inspect durable broker evidence",
	"V05": "observe external application effect before completion evidence closes",
	"V06": "drop consumer acknowledgement and verify completion remains distinct",
	"V07": "hold the bounded search fault window for at least 60 seconds",
	"V08": "exhaust the declared business-stage budget and verify fixed rejection",
	"V09": "hold the bounded telemetry fault window for at least 60 seconds",
	"V10": "exercise the platform or permission failure boundary",
	"V11": "restore real business data and reject old headers, cursor, cache, message, and completion generation",
	"V12": "cancel an in-flight query and verify owned cleanup and evidence closure",
}

// BusinessV1 owns the candidate-pinned V1 entry point. Preflight remains the
// short browser contract; formal mode executes the native case driver and
// records any unavailable fault control as an infrastructure failure.
func BusinessV1(root string, options Options) (returnErr error) {
	if options.Keep {
		return fmt.Errorf("V1 acceptance does not allow --keep")
	}
	if options.Manifest == "" {
		return fmt.Errorf("V1 acceptance requires --manifest")
	}
	if options.Receipt == "" {
		return fmt.Errorf("V1 acceptance requires --receipt")
	}
	if options.Preflight && options.VerifyEvidence {
		return fmt.Errorf("--preflight cannot be combined with --verify-evidence")
	}
	if options.Budget == "" {
		options.Budget = "30m"
	}
	budget, err := parseV1Budget(options.Budget)
	if err != nil {
		return err
	}
	manifestPath := resolvePath(root, options.Manifest)
	receiptPath := resolvePath(root, options.Receipt)
	if options.VerifyEvidence {
		if options.Publication != "" {
			return verifyV1Evidence(root, manifestPath, receiptPath, resolvePath(root, options.Publication))
		}
		return verifyV1Evidence(root, manifestPath, receiptPath, "")
	}
	return runV1Stack(root, options, manifestPath, receiptPath, budget)
}

func runV1Stack(root string, options Options, manifestPath, receiptPath string, budget time.Duration) (returnErr error) {
	session, err := harness.New(root, false)
	if err != nil {
		return err
	}
	defer func() {
		cleanupErr := session.Cleanup()
		if cleanupErr != nil {
			session.SetStatus("failed", cleanupErr)
			if returnErr == nil {
				returnErr = cleanupErr
			}
		}
		document := session.Receipt()
		if returnErr == nil {
			if err := receipt.ValidateV1(document); err != nil {
				returnErr = fmt.Errorf("V1 receipt self-validation failed: %w", err)
				session.SetStatus("failed", returnErr)
			}
		}
		if receiptErr := session.WriteReceipt(receiptPath); returnErr == nil && receiptErr != nil {
			returnErr = receiptErr
		}
	}()

	candidate, err := configurePinnedCandidate(session, manifestPath)
	if err != nil {
		session.SetStatus("failed", err)
		return err
	}
	rawDirectory := receiptPath + ".raw-" + session.Token
	if err := os.MkdirAll(filepath.Join(rawDirectory, "cases"), 0o700); err != nil {
		session.SetStatus("failed", err)
		return err
	}
	mode := receipt.V1ModeFormal
	if options.Preflight {
		mode = receipt.V1ModePreflight
	}
	evidence := newV1Evidence(candidate, mode, budget, rawDirectory)
	session.SetV1Evidence(evidence)
	session.SetValue("GOPULSE_ACCEPTANCE_BUDGET", options.Budget)
	deadline := time.Now().Add(budget)
	if err := ensureV1Budget(deadline, "start"); err != nil {
		session.SetStatus("failed", err)
		return err
	}
	if err := session.Preflight(); err != nil {
		session.SetStatus("failed", err)
		return err
	}
	session.MarkStarted()
	build := session.Compose("build", "acceptance")
	if build.ExitCode != 0 {
		err := fmt.Errorf("V1 acceptance image build failed with exit code %d", build.ExitCode)
		session.SetStatus("failed", err)
		return err
	}
	up := session.Compose("up", "--detach", "--wait", "--wait-timeout", "420")
	if up.ExitCode != 0 {
		err := fmt.Errorf("V1 candidate Compose startup failed with exit code %d", up.ExitCode)
		session.SetStatus("failed", err)
		return err
	}
	if err := ensureV1Budget(deadline, "Compose startup"); err != nil {
		session.SetStatus("failed", err)
		return err
	}
	if smoke := session.AcceptanceRun("", "e2e/compose-smoke.spec.ts"); smoke.ExitCode != 0 {
		err := fmt.Errorf("V1 Compose smoke acceptance failed with exit code %d", smoke.ExitCode)
		session.SetStatus("failed", err)
		return err
	}
	setup := session.AcceptanceRun("setup", "e2e/compose-observability.spec.ts", "GOPULSE_OBSERVABILITY_ADMIN_USERNAME", "GOPULSE_OBSERVABILITY_USER_USERNAME", "GOPULSE_OBSERVABILITY_PASSWORD")
	if setup.ExitCode != 0 {
		err := fmt.Errorf("V1 observability account setup failed with exit code %d", setup.ExitCode)
		session.SetStatus("failed", err)
		return err
	}
	if err := promoteAdmin(session); err != nil {
		session.SetStatus("failed", err)
		return err
	}
	if err := ensureV1Budget(deadline, "account setup"); err != nil {
		session.SetStatus("failed", err)
		return err
	}
	if err := runV1Baseline(session, &evidence, rawDirectory, deadline, mode); err != nil {
		session.SetV1Evidence(evidence)
		session.SetStatus("failed", err)
		return err
	}
	if mode == receipt.V1ModeFormal {
		if err := runV1Formal(session, &evidence, rawDirectory, deadline); err != nil {
			session.SetV1Evidence(evidence)
			session.SetStatus("failed", err)
			return err
		}
	}
	session.SetV1Evidence(evidence)
	session.SetStatus("passed", nil)
	return nil
}

func runV1Baseline(session *harness.Session, evidence *receipt.V1Evidence, rawDirectory string, deadline time.Time, mode string) error {
	caseID := "V01"
	session.SetValue("GOPULSE_V1_CASE_ID", caseID)
	session.SetValue("GOPULSE_V1_CASE_EVIDENCE", "/work/frontend/test-results/v1-case.json")
	hostPath := filepath.Join(rawDirectory, "cases", "V01.json")
	started := time.Now().UTC()
	scenario := "preflight"
	if mode == receipt.V1ModeFormal {
		scenario = "v1"
	}
	result := session.AcceptanceRunWithEvidence(scenario, "e2e/business-stages.spec.ts", hostPath, nil,
		"GOPULSE_OBSERVABILITY_ADMIN_USERNAME", "GOPULSE_OBSERVABILITY_USER_USERNAME", "GOPULSE_OBSERVABILITY_PASSWORD",
		"GOPULSE_V1_CASE_ID", "GOPULSE_V1_CASE_EVIDENCE", "GOPULSE_ACCEPTANCE_BUDGET")
	if result.ExitCode != 0 {
		return fmt.Errorf("V1 browser baseline failed with exit code %d", result.ExitCode)
	}
	if err := ensureV1Budget(deadline, "browser baseline"); err != nil {
		return err
	}
	raw, err := receipt.ReadRawEvidence(hostPath)
	if err != nil {
		return fmt.Errorf("read V1 browser evidence: %w", err)
	}
	checksum, err := receipt.DigestFile(hostPath)
	if err != nil {
		return fmt.Errorf("hash V1 browser evidence: %w", err)
	}
	finished := time.Now().UTC()
	item := receipt.V1Case{
		CaseID: caseID, Status: "passed", PlannedAction: v1PlannedActions[caseID], ActualAction: "created post and checked stage page plus precise Logs association",
		Identity: raw.Identity,
		Moments:  []receipt.Moment{{Name: "action_started", Actual: started.Format(time.RFC3339Nano)}, {Name: "stage_page_checked", Actual: raw.CapturedAt}, {Name: "finished", Actual: finished.Format(time.RFC3339Nano)}},
		Raw:      receipt.RawRecord{Path: "cases/V01.json", SHA256: checksum}, CleanupPassed: true,
	}
	for index := range evidence.Cases {
		if evidence.Cases[index].CaseID == caseID {
			evidence.Cases[index] = item
		}
	}
	return nil
}

func verifyV1Evidence(root, manifestPath, receiptPath, publicationPath string) error {
	version, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		return fmt.Errorf("read VERSION: %w", err)
	}
	revision, err := commandOutput(root, "git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	manifest, data, err := readPinnedManifest(manifestPath, strings.TrimSpace(string(version)), revision)
	if err != nil {
		return err
	}
	candidate, err := pinnedCandidateIdentity(manifestPath, manifest, data)
	if err != nil {
		return err
	}
	document, err := receipt.VerifyV1(receiptPath)
	if err != nil {
		return fmt.Errorf("verify V1 evidence: %w", err)
	}
	if document.V1.Candidate != candidate || document.V1.Candidate.ManifestPath != manifestPath {
		return fmt.Errorf("V1 receipt is bound to a different candidate")
	}
	if publicationPath != "" {
		if err := receipt.WritePublication(publicationPath, document); err != nil {
			return fmt.Errorf("write V1 publication: %w", err)
		}
		if err := receipt.VerifyPublication(publicationPath); err != nil {
			return fmt.Errorf("verify V1 publication: %w", err)
		}
	}
	if document.Status != "passed" {
		return fmt.Errorf("V1 evidence is %s, not passed", document.Status)
	}
	fmt.Printf("[gopulse-acceptance] PASS: verified V1 evidence receipt=%s\n", receiptPath)
	return nil
}

func newV1Evidence(candidate receipt.CandidateIdentity, mode string, budget time.Duration, rawDirectory string) receipt.V1Evidence {
	ids := []string{"BUSINESS_ASYNC_MAX_OPEN_REQUIREMENTS", "GOPULSE_ACCEPTANCE_BUDGET", "GOPULSE_OBSERVABILITY_ADMIN_USERNAME", "GOPULSE_OBSERVABILITY_PASSWORD", "GOPULSE_OBSERVABILITY_USER_USERNAME", "GOPULSE_V1_CASE_EVIDENCE", "GOPULSE_V1_CASE_ID"}
	sort.Strings(ids)
	clients, maxInFlight := 1, 1
	if mode == receipt.V1ModeFormal {
		clients, maxInFlight = 10, 10
	}
	evidence := receipt.V1Evidence{Version: receipt.V1EvidenceVersion, Mode: mode, Candidate: candidate, Source: receipt.V1SourcePinned, Contract: receipt.V1Contract, Tool: candidate.Tool, ConfigSHA256: candidate.ConfigSHA256, RawDirectory: rawDirectory, Environment: ids, Load: receipt.LoadProfile{Clients: clients, MaxInFlight: maxInFlight, BudgetSeconds: int64(budget / time.Second)}}
	caseIDs := []string{"V01"}
	if mode == receipt.V1ModeFormal {
		caseIDs = append([]string(nil), receipt.V1CaseIDs...)
	}
	for _, caseID := range caseIDs {
		evidence.Cases = append(evidence.Cases, receipt.V1Case{CaseID: caseID, Status: "not_started", PlannedAction: v1PlannedActions[caseID]})
	}
	return evidence
}

func parseV1Budget(value string) (time.Duration, error) {
	duration, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil || duration <= 0 || duration > 24*time.Hour {
		return 0, fmt.Errorf("BUDGET must be a finite positive duration no longer than 24h")
	}
	return duration, nil
}

func ensureV1Budget(deadline time.Time, step string) error {
	if time.Now().After(deadline) {
		return fmt.Errorf("V1 budget exhausted before %s; cleanup is still required", step)
	}
	return nil
}
