package receipt

// V1 evidence is deliberately kept in the acceptance module. It is a private
// contract for the frozen business-stage gate, not an application API.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	V1EvidenceVersion   = "1"
	V1ModePreflight     = "preflight"
	V1ModeFormal        = "formal"
	V1SourcePinned      = "pinned-candidate"
	V1Contract          = "phase24-v1"
	V1PublicationSchema = "gopulse.acceptance.v1.publication"
)

var V1CaseIDs = []string{"V01", "V02", "V03", "V04", "V05", "V06", "V07", "V08", "V09", "V10", "V11", "V12"}

var (
	v1UUID       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	v1Revision   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	v1Semver     = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	v1Digest     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	v1RawPath    = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	sensitiveKey = regexp.MustCompile(`(?i)(password|passwd|token|secret|cookie|authorization|credential|request[_-]?digest|sql|payload|body|content)`)
)

type CandidateIdentity struct {
	Version        string `json:"version"`
	Revision       string `json:"revision"`
	ManifestPath   string `json:"manifest_path"`
	ManifestSHA256 string `json:"manifest_sha256"`
	BundlePath     string `json:"bundle_path"`
	BundleSHA256   string `json:"bundle_sha256"`
	SourceRevision string `json:"source_revision"`
	ContractSHA256 string `json:"contract_sha256"`
	Tool           string `json:"tool"`
	ToolRevision   string `json:"tool_revision"`
	ConfigSHA256   string `json:"config_sha256"`
}

type V1Identity struct {
	DataEpoch               string `json:"data_epoch"`
	CommandID               string `json:"command_id"`
	EventID                 string `json:"event_id"`
	PostID                  uint64 `json:"post_id"`
	ProjectionGeneration    string `json:"projection_generation,omitempty"`
	RoutingContractRevision string `json:"routing_contract_revision,omitempty"`
}

type RawRecord struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type Moment struct {
	Name    string `json:"name"`
	Planned string `json:"planned,omitempty"`
	Actual  string `json:"actual"`
}

type V1Subwindow struct {
	Index               int    `json:"index"`
	FaultSeconds        int64  `json:"fault_seconds"`
	RecoverySeconds     int64  `json:"recovery_seconds"`
	FaultStartedAt      string `json:"fault_started_at,omitempty"`
	FaultEndedAt        string `json:"fault_ended_at,omitempty"`
	DependencyReadyAt   string `json:"dependency_ready_at,omitempty"`
	ObligationsClosedAt string `json:"obligations_closed_at,omitempty"`
	VisibleAt           string `json:"visible_at,omitempty"`
}

type V1Case struct {
	CaseID          string        `json:"case_id"`
	Status          string        `json:"status"`
	FailureClass    string        `json:"failure_class,omitempty"`
	PlannedAction   string        `json:"planned_action"`
	ActualAction    string        `json:"actual_action,omitempty"`
	Identity        V1Identity    `json:"identity"`
	Moments         []Moment      `json:"moments,omitempty"`
	Subwindows      []V1Subwindow `json:"subwindows,omitempty"`
	WindowSeconds   int64         `json:"window_seconds,omitempty"`
	RecoverySeconds int64         `json:"recovery_seconds,omitempty"`
	Raw             RawRecord     `json:"raw,omitempty"`
	Child           *RawRecord    `json:"child,omitempty"`
	CleanupPassed   bool          `json:"cleanup_passed"`
	CleanupError    string        `json:"cleanup_error,omitempty"`
}

type LoadProfile struct {
	Clients       int   `json:"clients"`
	MaxInFlight   int   `json:"max_in_flight"`
	BudgetSeconds int64 `json:"budget_seconds"`
}

type V1Evidence struct {
	Version       string            `json:"version"`
	Mode          string            `json:"mode"`
	Candidate     CandidateIdentity `json:"candidate"`
	Source        string            `json:"source"`
	Contract      string            `json:"contract"`
	Tool          string            `json:"tool"`
	ConfigSHA256  string            `json:"config_sha256"`
	RawDirectory  string            `json:"raw_directory"`
	Environment   []string          `json:"environment"`
	Load          LoadProfile       `json:"load"`
	Cases         []V1Case          `json:"cases"`
	CleanupPassed bool              `json:"cleanup_passed"`
	CleanupError  string            `json:"cleanup_error,omitempty"`
}

type RawChecks struct {
	Created         bool `json:"created"`
	StagePage       bool `json:"stage_page"`
	PreciseLogsLink bool `json:"precise_logs_link"`
}

// RawFact is a small numeric observation emitted by a formal case. Keeping
// this list typed and bounded lets raw evidence carry counts without becoming
// a dump of request bodies, SQL, credentials, or dependency payloads.
type RawFact struct {
	Name  string `json:"name"`
	Value int64  `json:"value"`
}

type RawEvidence struct {
	CaseID     string     `json:"case_id"`
	Identity   V1Identity `json:"identity"`
	Checks     RawChecks  `json:"checks"`
	CapturedAt string     `json:"captured_at"`
	Facts      []RawFact  `json:"facts,omitempty"`
}

type ChildReceipt struct {
	CaseID            string     `json:"case_id"`
	TaskID            string     `json:"task_id"`
	Status            string     `json:"status"`
	Identity          V1Identity `json:"identity"`
	StartedAt         string     `json:"started_at"`
	CancelRequestedAt string     `json:"cancel_requested_at"`
	FinishedAt        string     `json:"finished_at"`
	CompletedStages   []string   `json:"completed_stages,omitempty"`
	CleanupRemoved    bool       `json:"cleanup_removed"`
	OwnedResource     string     `json:"owned_resource"`
}

type PublicationCase struct {
	CaseID          string `json:"case_id"`
	Status          string `json:"status"`
	FailureClass    string `json:"failure_class,omitempty"`
	DataEpoch       string `json:"data_epoch,omitempty"`
	CommandID       string `json:"command_id,omitempty"`
	EventID         string `json:"event_id,omitempty"`
	PostID          uint64 `json:"post_id,omitempty"`
	WindowSeconds   int64  `json:"window_seconds,omitempty"`
	RecoverySeconds int64  `json:"recovery_seconds,omitempty"`
}

type Publication struct {
	Schema      string            `json:"schema"`
	Status      string            `json:"status"`
	Version     string            `json:"version"`
	Revision    string            `json:"revision"`
	Candidate   CandidateIdentity `json:"candidate"`
	Cases       []PublicationCase `json:"cases"`
	GeneratedAt string            `json:"generated_at"`
}

func ValidateV1(document Document) error {
	if document.Schema != Schema {
		return fmt.Errorf("receipt schema must be %s", Schema)
	}
	if document.V1 == nil {
		return errors.New("v1 evidence is missing")
	}
	if document.Status != "passed" && document.Status != "failed" && document.Status != "incomplete" {
		return fmt.Errorf("invalid receipt status %q", document.Status)
	}
	evidence := document.V1
	if evidence.Version != V1EvidenceVersion || (evidence.Mode != V1ModePreflight && evidence.Mode != V1ModeFormal) {
		return errors.New("invalid V1 evidence version or mode")
	}
	if evidence.Source != V1SourcePinned || evidence.Contract != V1Contract || evidence.Tool == "" || evidence.ConfigSHA256 == "" || !v1Digest.MatchString(evidence.ConfigSHA256) {
		return errors.New("V1 candidate/tool contract is incomplete")
	}
	if !document.CleanupPassed || !evidence.CleanupPassed || evidence.CleanupError != "" {
		return errors.New("V1 cleanup did not pass")
	}
	if err := validateCandidate(evidence.Candidate, document); err != nil {
		return err
	}
	if evidence.RawDirectory == "" || !filepath.IsAbs(evidence.RawDirectory) {
		return errors.New("V1 raw directory must be an absolute private path")
	}
	if len(evidence.Environment) == 0 || !sort.StringsAreSorted(evidence.Environment) || hasDuplicate(evidence.Environment) {
		return errors.New("V1 environment names must be sorted and unique")
	}
	if evidence.Load.Clients <= 0 || evidence.Load.MaxInFlight <= 0 || evidence.Load.BudgetSeconds <= 0 {
		return errors.New("V1 load profile is invalid")
	}
	allowed := make(map[string]bool, len(V1CaseIDs))
	for _, id := range V1CaseIDs {
		allowed[id] = true
	}
	seen := map[string]bool{}
	for _, item := range evidence.Cases {
		if !allowed[item.CaseID] || seen[item.CaseID] {
			return fmt.Errorf("V1 case %q is unknown or duplicated", item.CaseID)
		}
		seen[item.CaseID] = true
		if err := validateCase(item, evidence.Mode); err != nil {
			return err
		}
	}
	if evidence.Mode == V1ModePreflight {
		if len(evidence.Cases) != 1 || evidence.Cases[0].CaseID != "V01" {
			return errors.New("preflight must contain exactly one V01 case")
		}
		if document.Status == "passed" && evidence.Cases[0].Status != "passed" {
			return errors.New("successful preflight must have a passed V01 case")
		}
	} else {
		if len(evidence.Cases) != len(V1CaseIDs) {
			return fmt.Errorf("formal V1 evidence must list all %d cases", len(V1CaseIDs))
		}
		if document.Status == "passed" {
			for _, item := range evidence.Cases {
				if item.Status != "passed" {
					return fmt.Errorf("formal V1 receipt cannot pass with case %s=%s", item.CaseID, item.Status)
				}
			}
		}
	}
	if document.Status == "passed" && document.Error != "" {
		return errors.New("passed receipt cannot contain an error")
	}
	return nil
}

func validateCandidate(candidate CandidateIdentity, document Document) error {
	if !v1Semver.MatchString(candidate.Version) || candidate.Version != document.Version || !v1Revision.MatchString(candidate.Revision) || candidate.Revision != document.Revision {
		return errors.New("candidate version/revision does not match receipt")
	}
	if candidate.ManifestPath == "" || !v1Digest.MatchString(candidate.ManifestSHA256) || candidate.BundlePath == "" || !v1Digest.MatchString(candidate.BundleSHA256) || !v1Revision.MatchString(candidate.SourceRevision) || !v1Digest.MatchString(candidate.ContractSHA256) || candidate.Tool == "" || !v1Revision.MatchString(candidate.ToolRevision) || !v1Digest.MatchString(candidate.ConfigSHA256) {
		return errors.New("candidate identity is incomplete")
	}
	return nil
}

func validateCase(item V1Case, mode string) error {
	switch item.Status {
	case "passed":
		if item.FailureClass != "" || item.ActualAction == "" || !item.CleanupPassed {
			return fmt.Errorf("passed V1 case %s has incomplete action or cleanup facts", item.CaseID)
		}
	case "failed":
		if item.FailureClass == "" || item.ActualAction == "" || !item.CleanupPassed {
			return fmt.Errorf("failed V1 case %s has incomplete failure or cleanup facts", item.CaseID)
		}
	case "not_started":
		if item.ActualAction != "" || item.Raw.Path != "" || item.Raw.SHA256 != "" || item.Child != nil || len(item.Moments) != 0 || len(item.Subwindows) != 0 || item.CleanupPassed {
			return fmt.Errorf("not-started V1 case %s contains completion evidence", item.CaseID)
		}
	default:
		return fmt.Errorf("V1 case %s has invalid status %q", item.CaseID, item.Status)
	}
	if item.PlannedAction == "" {
		return fmt.Errorf("V1 case %s has no planned action", item.CaseID)
	}
	if item.Status == "not_started" {
		return nil
	}
	if len(item.Subwindows) > 0 && item.CaseID != "V07" {
		return fmt.Errorf("V1 case %s has unexpected subwindow evidence", item.CaseID)
	}
	if !v1UUID.MatchString(item.Identity.DataEpoch) || !v1UUID.MatchString(item.Identity.CommandID) || !v1UUID.MatchString(item.Identity.EventID) || item.Identity.PostID == 0 {
		return fmt.Errorf("V1 case %s has incomplete business identity", item.CaseID)
	}
	if item.WindowSeconds < 0 || item.RecoverySeconds < 0 {
		return fmt.Errorf("V1 case %s has a negative timing value", item.CaseID)
	}
	if mode == V1ModeFormal && item.Status == "passed" {
		switch item.CaseID {
		case "V09":
			if item.WindowSeconds < 60 {
				return fmt.Errorf("V1 case %s shortened its required 60 second fault window", item.CaseID)
			}
		case "V07":
			if item.WindowSeconds < 60 {
				return fmt.Errorf("V1 case %s shortened its required 60 second fault window", item.CaseID)
			}
			if len(item.Subwindows) != 2 {
				return errors.New("V1 case V07 must record two recovered subwindows")
			}
			for _, subwindow := range item.Subwindows {
				if subwindow.FaultSeconds < 60 || subwindow.RecoverySeconds > 120 {
					return fmt.Errorf("V1 case V07 subwindow %d violates its timing contract", subwindow.Index)
				}
			}
		case "V11":
			if item.RecoverySeconds < 60 {
				return fmt.Errorf("V1 case %s shortened its recovery window", item.CaseID)
			}
		}
	}
	if err := validateSubwindows(item.Subwindows, mode == V1ModeFormal && item.Status == "passed" && item.CaseID == "V07"); err != nil {
		return fmt.Errorf("V1 case %s subwindows: %w", item.CaseID, err)
	}
	if len(item.Moments) < 2 {
		return fmt.Errorf("V1 case %s has too few moments", item.CaseID)
	}
	seen := map[string]bool{}
	var previous time.Time
	for _, moment := range item.Moments {
		if moment.Name == "" || seen[moment.Name] || !validTime(moment.Actual) {
			return fmt.Errorf("V1 case %s has invalid moments", item.CaseID)
		}
		seen[moment.Name] = true
		at, _ := time.Parse(time.RFC3339Nano, moment.Actual)
		if !previous.IsZero() && at.Before(previous) {
			return fmt.Errorf("V1 case %s moments are not ordered", item.CaseID)
		}
		previous = at
	}
	if !v1RawPath.MatchString(item.Raw.Path) || strings.HasPrefix(item.Raw.Path, "/") || strings.Contains(item.Raw.Path, "..") || !v1Digest.MatchString(item.Raw.SHA256) {
		return fmt.Errorf("V1 case %s raw evidence reference is invalid", item.CaseID)
	}
	if item.CleanupError != "" {
		return fmt.Errorf("V1 case %s has a cleanup error", item.CaseID)
	}
	if item.CaseID == "V12" && item.Child == nil {
		return errors.New("V12 requires a child receipt")
	}
	if item.CaseID != "V12" && item.Child != nil {
		return fmt.Errorf("V1 case %s has an unexpected child receipt", item.CaseID)
	}
	if item.Child != nil && (!v1RawPath.MatchString(item.Child.Path) || strings.HasPrefix(item.Child.Path, "/") || strings.Contains(item.Child.Path, "..") || !v1Digest.MatchString(item.Child.SHA256)) {
		return fmt.Errorf("V1 case %s child receipt reference is invalid", item.CaseID)
	}
	return nil
}

func validateSubwindows(subwindows []V1Subwindow, requireComplete bool) error {
	seen := map[int]bool{}
	for _, subwindow := range subwindows {
		if subwindow.Index < 0 || seen[subwindow.Index] || subwindow.FaultSeconds < 0 || subwindow.RecoverySeconds < 0 {
			return fmt.Errorf("invalid subwindow %d", subwindow.Index)
		}
		seen[subwindow.Index] = true
		controlMoments := []struct {
			name string
			at   string
		}{
			{"fault_started_at", subwindow.FaultStartedAt},
			{"fault_ended_at", subwindow.FaultEndedAt},
			{"dependency_ready_at", subwindow.DependencyReadyAt},
		}
		var previous time.Time
		for _, moment := range controlMoments {
			if moment.at == "" {
				if requireComplete {
					return fmt.Errorf("subwindow %d is missing %s", subwindow.Index, moment.name)
				}
				continue
			}
			if !validTime(moment.at) {
				return fmt.Errorf("subwindow %d has invalid %s", subwindow.Index, moment.name)
			}
			at, _ := time.Parse(time.RFC3339Nano, moment.at)
			if !previous.IsZero() && at.Before(previous) {
				return fmt.Errorf("subwindow %d moments are not ordered", subwindow.Index)
			}
			previous = at
		}
		for _, moment := range []struct {
			name string
			at   string
		}{
			{"obligations_closed_at", subwindow.ObligationsClosedAt},
			{"visible_at", subwindow.VisibleAt},
		} {
			if moment.at == "" {
				if requireComplete {
					return fmt.Errorf("subwindow %d is missing %s", subwindow.Index, moment.name)
				}
				continue
			}
			if !validTime(moment.at) {
				return fmt.Errorf("subwindow %d has invalid %s", subwindow.Index, moment.name)
			}
			at, _ := time.Parse(time.RFC3339Nano, moment.at)
			if !previous.IsZero() && at.Before(previous) {
				return fmt.Errorf("subwindow %d %s precedes dependency readiness", subwindow.Index, moment.name)
			}
		}
		if requireComplete && (subwindow.FaultSeconds < 60 || subwindow.RecoverySeconds > 120) {
			return fmt.Errorf("subwindow %d violates the 60 second fault and 120 second recovery limits", subwindow.Index)
		}
	}
	if requireComplete {
		if len(subwindows) != 2 || !seen[0] || !seen[1] {
			return errors.New("two subwindows with indexes 0 and 1 are required")
		}
	}
	return nil
}

func VerifyV1(path string) (Document, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Document{}, fmt.Errorf("read V1 receipt: %w", err)
	}
	var document Document
	if err := decodeStrict(raw, &document); err != nil {
		return Document{}, fmt.Errorf("parse V1 receipt: %w", err)
	}
	if err := ValidateV1(document); err != nil {
		return Document{}, err
	}
	root, err := filepath.EvalSymlinks(document.V1.RawDirectory)
	if err != nil {
		return Document{}, fmt.Errorf("resolve V1 raw directory: %w", err)
	}
	rootInfo, err := os.Lstat(document.V1.RawDirectory)
	if err != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() || rootInfo.Mode().Perm()&0o077 != 0 {
		return Document{}, errors.New("V1 raw directory is not a private regular directory")
	}
	for _, item := range document.V1.Cases {
		if item.Status == "not_started" {
			continue
		}
		relative := filepath.Clean(filepath.FromSlash(item.Raw.Path))
		if filepath.IsAbs(relative) || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return Document{}, fmt.Errorf("V1 case %s raw path escapes its directory", item.CaseID)
		}
		file := filepath.Join(root, relative)
		resolved, err := filepath.EvalSymlinks(file)
		if err != nil {
			return Document{}, fmt.Errorf("resolve V1 raw evidence for %s: %w", item.CaseID, err)
		}
		if !within(root, resolved) {
			return Document{}, fmt.Errorf("V1 case %s raw path is outside its directory", item.CaseID)
		}
		fileInfo, err := os.Lstat(file)
		if err != nil || fileInfo.Mode()&os.ModeSymlink != 0 || !fileInfo.Mode().IsRegular() || fileInfo.Mode().Perm()&0o077 != 0 {
			return Document{}, fmt.Errorf("V1 case %s raw evidence is not a private regular file", item.CaseID)
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			return Document{}, fmt.Errorf("read V1 raw evidence for %s: %w", item.CaseID, err)
		}
		if sum(data) != item.Raw.SHA256 {
			return Document{}, fmt.Errorf("V1 raw evidence checksum mismatch for %s", item.CaseID)
		}
		var evidence RawEvidence
		if err := decodeStrict(data, &evidence); err != nil {
			return Document{}, fmt.Errorf("parse V1 raw evidence for %s: %w", item.CaseID, err)
		}
		if err := validateRawEvidence(evidence); err != nil {
			return Document{}, fmt.Errorf("validate V1 raw evidence for %s: %w", item.CaseID, err)
		}
		if evidence.CaseID != item.CaseID || evidence.Identity != item.Identity || !evidence.Checks.Created {
			return Document{}, fmt.Errorf("V1 raw evidence does not match case %s", item.CaseID)
		}
		if item.Status == "passed" && item.CaseID != "V02" && (!evidence.Checks.StagePage || !evidence.Checks.PreciseLogsLink) {
			return Document{}, fmt.Errorf("V1 passed raw evidence is incomplete for case %s", item.CaseID)
		}
		if item.Child != nil {
			childRelative := filepath.Clean(filepath.FromSlash(item.Child.Path))
			if filepath.IsAbs(childRelative) || childRelative == "." || childRelative == ".." || strings.HasPrefix(childRelative, ".."+string(filepath.Separator)) {
				return Document{}, fmt.Errorf("V1 child receipt path escapes its directory for %s", item.CaseID)
			}
			childPath := filepath.Join(root, childRelative)
			resolvedChild, err := filepath.EvalSymlinks(childPath)
			if err != nil || !within(root, resolvedChild) {
				return Document{}, fmt.Errorf("resolve V1 child receipt for %s", item.CaseID)
			}
			childInfo, err := os.Lstat(childPath)
			if err != nil || childInfo.Mode()&os.ModeSymlink != 0 || !childInfo.Mode().IsRegular() || childInfo.Mode().Perm()&0o077 != 0 {
				return Document{}, fmt.Errorf("V1 child receipt is not a private regular file for %s", item.CaseID)
			}
			childRaw, err := os.ReadFile(resolvedChild)
			if err != nil || sum(childRaw) != item.Child.SHA256 {
				return Document{}, fmt.Errorf("V1 child receipt checksum mismatch for %s", item.CaseID)
			}
			var child ChildReceipt
			if err := decodeStrict(childRaw, &child); err != nil {
				return Document{}, fmt.Errorf("parse V1 child receipt for %s: %w", item.CaseID, err)
			}
			if err := validateChildReceipt(child, item); err != nil {
				return Document{}, fmt.Errorf("validate V1 child receipt for %s: %w", item.CaseID, err)
			}
		}
	}
	return document, nil
}

func ReadRawEvidence(path string) (RawEvidence, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RawEvidence{}, err
	}
	var evidence RawEvidence
	if err := decodeStrict(data, &evidence); err != nil {
		return RawEvidence{}, err
	}
	if err := validateRawEvidence(evidence); err != nil {
		return RawEvidence{}, err
	}
	return evidence, nil
}

// WriteRawEvidence publishes one private raw case file atomically. Formal
// runners use this instead of writing JSON themselves so preflight and formal
// evidence have identical permissions and publication semantics.
func WriteRawEvidence(path string, evidence RawEvidence) error {
	if err := validateRawEvidence(evidence); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".raw-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func WriteChildReceipt(path string, child ChildReceipt) error {
	if err := validateChildReceiptShape(child); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(child, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".child-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func validateChildReceiptShape(child ChildReceipt) error {
	if child.CaseID != "V12" || !v1UUID.MatchString(child.TaskID) || (child.Status != "canceled" && child.Status != "incomplete") || !v1UUID.MatchString(child.Identity.DataEpoch) || !v1UUID.MatchString(child.Identity.CommandID) || !v1UUID.MatchString(child.Identity.EventID) || child.Identity.PostID == 0 || !v1RawPath.MatchString(child.OwnedResource) || !child.CleanupRemoved || !validTime(child.StartedAt) || !validTime(child.CancelRequestedAt) || !validTime(child.FinishedAt) {
		return errors.New("child receipt is incomplete")
	}
	started, _ := time.Parse(time.RFC3339Nano, child.StartedAt)
	cancelled, _ := time.Parse(time.RFC3339Nano, child.CancelRequestedAt)
	finished, _ := time.Parse(time.RFC3339Nano, child.FinishedAt)
	if cancelled.Before(started) || finished.Before(cancelled) {
		return errors.New("child receipt timestamps are not ordered")
	}
	return nil
}

func validateChildReceipt(child ChildReceipt, item V1Case) error {
	if err := validateChildReceiptShape(child); err != nil {
		return err
	}
	if child.Identity != item.Identity {
		return errors.New("child receipt identity does not match its case")
	}
	return nil
}

func DigestFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return sum(data), nil
}

func WritePublication(path string, document Document) error {
	if err := ValidateV1(document); err != nil {
		return err
	}
	publication := Publication{Schema: V1PublicationSchema, Status: document.Status, Version: document.Version, Revision: document.Revision, Candidate: document.V1.Candidate, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	for _, item := range document.V1.Cases {
		publication.Cases = append(publication.Cases, PublicationCase{CaseID: item.CaseID, Status: item.Status, FailureClass: item.FailureClass, DataEpoch: item.Identity.DataEpoch, CommandID: item.Identity.CommandID, EventID: item.Identity.EventID, PostID: item.Identity.PostID, WindowSeconds: item.WindowSeconds, RecoverySeconds: item.RecoverySeconds})
	}
	raw, err := json.MarshalIndent(publication, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".publication-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func VerifyPublication(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var publication Publication
	if err := decodeStrict(raw, &publication); err != nil {
		return err
	}
	if publication.Schema != V1PublicationSchema || (publication.Status != "passed" && publication.Status != "failed" && publication.Status != "incomplete") || !v1Semver.MatchString(publication.Version) || !v1Revision.MatchString(publication.Revision) || !validTime(publication.GeneratedAt) {
		return errors.New("invalid V1 publication")
	}
	return nil
}

func validateSensitive(value any) error {
	var walk func(any) error
	walk = func(current any) error {
		switch item := current.(type) {
		case map[string]any:
			for key, child := range item {
				if sensitiveKey.MatchString(key) {
					return fmt.Errorf("sensitive field %q is not allowed", key)
				}
				if err := walk(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range item {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(value)
}

func decodeStrict(raw []byte, target any) error {
	if err := validateSensitiveJSON(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func validateSensitiveJSON(raw []byte) error {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	return validateSensitive(value)
}

var v1RawFactNames = map[string]bool{
	"applications":                   true,
	"business_independent":           true,
	"broker_received":                true,
	"broker_confirmed":               true,
	"broker_published":               true,
	"cancelled":                      true,
	"command_result_available":       true,
	"commit_observed":                true,
	"commit_result_dropped":          true,
	"completion_blocked":             true,
	"completion_present":             true,
	"completion_disconnect":          true,
	"completion_effect_observed":     true,
	"completion_response_dropped":    true,
	"commands":                       true,
	"conflicts":                      true,
	"cleanup_removed":                true,
	"child_cleanup_removed":          true,
	"child_incomplete":               true,
	"consumer_ack_dropped":           true,
	"consumer_delivery_received":     true,
	"consumer_disconnect":            true,
	"consumer_redelivered":           true,
	"consumer_redelivery_ack":        true,
	"epoch_changed":                  true,
	"es_effect_observed":             true,
	"es_response_dropped":            true,
	"fault_requests":                 true,
	"fault_windows":                  true,
	"frontend_routes":                true,
	"http_success":                   true,
	"http_unavailable":               true,
	"internal_denied":                true,
	"low_water_limit":                true,
	"mysql_disconnect":               true,
	"old_epoch_rejected":             true,
	"old_generation_not_completed":   true,
	"open_requirements_after":        true,
	"open_requirements_before":       true,
	"other_commands":                 true,
	"other_events":                   true,
	"other_identity_distinct":        true,
	"other_posts":                    true,
	"other_requirements":             true,
	"other_user_created":             true,
	"owner_distinct":                 true,
	"platform_unavailable":           true,
	"posts":                          true,
	"projection_present":             true,
	"generation_changed":             true,
	"publisher_confirm_dropped":      true,
	"publisher_confirmed":            true,
	"publisher_disconnect":           true,
	"publisher_received":             true,
	"redelivered":                    true,
	"rejected":                       true,
	"recovery_seconds":               true,
	"role_demoted":                   true,
	"role_promoted":                  true,
	"response_loss_http_unavailable": true,
	"response_loss_replayed":         true,
	"response_loss_unique_events":    true,
	"restore_verified":               true,
	"requirements":                   true,
	"requests":                       true,
	"search_available":               true,
	"telemetry_drop_observed":        true,
	"telemetry_export_observed":      true,
	"telemetry_retry_observed":       true,
	"telemetry_unknown":              true,
	"unauthenticated_denied":         true,
	"unique_events":                  true,
	"unique_posts":                   true,
	"wrong_private_credential":       true,
}

func validateRawEvidence(evidence RawEvidence) error {
	if evidence.CaseID == "" || !v1UUID.MatchString(evidence.Identity.DataEpoch) || !v1UUID.MatchString(evidence.Identity.CommandID) || !v1UUID.MatchString(evidence.Identity.EventID) || evidence.Identity.PostID == 0 || !validTime(evidence.CapturedAt) {
		return errors.New("V1 raw evidence identity or timestamp is invalid")
	}
	seen := make(map[string]bool, len(evidence.Facts))
	for _, fact := range evidence.Facts {
		if !v1RawFactNames[fact.Name] || seen[fact.Name] || fact.Value < 0 {
			return fmt.Errorf("V1 raw fact %q is invalid", fact.Name)
		}
		seen[fact.Name] = true
	}
	return nil
}

func validTime(value string) bool {
	if value == "" {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, value)
	return err == nil
}

func hasDuplicate(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index] == values[index-1] {
			return true
		}
	}
	return false
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func sum(raw []byte) string {
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}
