package receipt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteAtomicPublishesPrivateSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "receipt.json")
	document := New("gopulse-accept-012345abcdef", "2.5.5", "revision")
	if err := WriteAtomic(path, document); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("receipt mode = %o", info.Mode().Perm())
	}
	var decoded Document
	if raw, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	} else if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Schema != Schema || decoded.Project == "" || decoded.FinishedAt == "" {
		t.Fatalf("invalid receipt: %+v", decoded)
	}
}

func validV1Document(t *testing.T) (Document, string) {
	t.Helper()
	rawDirectory := filepath.Join(t.TempDir(), "raw")
	if err := os.MkdirAll(filepath.Join(rawDirectory, "cases"), 0o700); err != nil {
		t.Fatal(err)
	}
	identity := V1Identity{DataEpoch: "123e4567-e89b-42d3-a456-426614174000", CommandID: "123e4567-e89b-42d3-a456-426614174001", EventID: "123e4567-e89b-42d3-a456-426614174002", PostID: 7}
	raw, err := json.Marshal(RawEvidence{CaseID: "V01", Identity: identity, Checks: RawChecks{Created: true, StagePage: true, PreciseLogsLink: true}, CapturedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	rawPath := filepath.Join(rawDirectory, "cases", "V01.json")
	if err := os.WriteFile(rawPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	digest, err := DigestFile(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	document := New("gopulse-accept-012345abcdef", "2.6.3", strings.Repeat("a", 40))
	document.Status = "passed"
	document.V1 = &V1Evidence{
		Version: V1EvidenceVersion, Mode: V1ModePreflight, Candidate: CandidateIdentity{
			Version: "2.6.3", Revision: strings.Repeat("a", 40), ManifestPath: "/private/manifest.json", ManifestSHA256: "sha256:" + strings.Repeat("1", 64), BundlePath: "/private/bundle.tar.gz", BundleSHA256: "sha256:" + strings.Repeat("2", 64), SourceRevision: strings.Repeat("a", 40), ContractSHA256: "sha256:" + strings.Repeat("3", 64), Tool: "gopulse-acceptance", ToolRevision: strings.Repeat("a", 40), ConfigSHA256: "sha256:" + strings.Repeat("4", 64),
		}, Source: V1SourcePinned, Contract: V1Contract, Tool: "gopulse-acceptance", ConfigSHA256: "sha256:" + strings.Repeat("4", 64), RawDirectory: rawDirectory, Environment: []string{"GOPULSE_ACCEPTANCE_BUDGET", "GOPULSE_V1_CASE_ID"}, Load: LoadProfile{Clients: 1, MaxInFlight: 1, BudgetSeconds: 1800}, Cases: []V1Case{{CaseID: "V01", Status: "passed", PlannedAction: "create post", ActualAction: "created and checked", Identity: identity, Moments: []Moment{{Name: "start", Actual: now.Format(time.RFC3339Nano)}, {Name: "finish", Actual: now.Add(time.Second).Format(time.RFC3339Nano)}}, Raw: RawRecord{Path: "cases/V01.json", SHA256: digest}, CleanupPassed: true}}, CleanupPassed: true,
	}
	return document, rawDirectory
}

func TestV1EvidenceRejectsShortWindowAndBadHash(t *testing.T) {
	document, _ := validV1Document(t)
	formal := *document.V1
	formal.Mode = V1ModeFormal
	formal.Cases = make([]V1Case, 0, len(V1CaseIDs))
	for _, id := range V1CaseIDs {
		item := V1Case{CaseID: id, Status: "not_started", PlannedAction: "planned"}
		if id == "V07" {
			item = document.V1.Cases[0]
			item.CaseID = id
			item.WindowSeconds = 1
		}
		formal.Cases = append(formal.Cases, item)
	}
	document.V1 = &formal
	document.Status = "failed"
	if err := ValidateV1(document); err == nil || !strings.Contains(err.Error(), "shortened") {
		t.Fatalf("short formal window was accepted: %v", err)
	}
	document, _ = validV1Document(t)
	document.V1.Cases[0].Raw.SHA256 = "sha256:" + strings.Repeat("f", 64)
	path := filepath.Join(t.TempDir(), "receipt.json")
	if err := WriteAtomic(path, document); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyV1(path); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("bad raw checksum was accepted: %v", err)
	}
}

func TestV1PublicationIsWhitelisted(t *testing.T) {
	document, _ := validV1Document(t)
	path := filepath.Join(t.TempDir(), "publication.json")
	if err := WritePublication(path, document); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPublication(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "raw_directory") || strings.Contains(string(raw), "password") {
		t.Fatalf("publication contains private fields: %s", raw)
	}
}

func TestV1SensitiveFieldIsRejected(t *testing.T) {
	document, _ := validV1Document(t)
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data[:len(data)-1], []byte(`,"request_digest":"secret"}`)...)
	path := filepath.Join(t.TempDir(), "receipt.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyV1(path); err == nil || !strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("sensitive field was accepted: %v", err)
	}
}

func TestFormalRawFactsAreBoundedAndFailedCasesMayPreservePartialChecks(t *testing.T) {
	document, rawDirectory := validV1Document(t)
	document.Status = "failed"
	document.V1.Cases[0].Status = "failed"
	document.V1.Cases[0].FailureClass = "infrastructure_failure"
	document.V1.Cases[0].ActualAction = "created the identity before the controlled fault was unavailable"
	evidence := RawEvidence{CaseID: "V01", Identity: document.V1.Cases[0].Identity, Checks: RawChecks{Created: true}, CapturedAt: time.Now().UTC().Format(time.RFC3339Nano), Facts: []RawFact{{Name: "requests", Value: 1}, {Name: "commands", Value: 1}}}
	path := filepath.Join(rawDirectory, "cases", "V01.json")
	if err := WriteRawEvidence(path, evidence); err != nil {
		t.Fatal(err)
	}
	digest, err := DigestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	document.V1.Cases[0].Raw.SHA256 = digest
	receiptPath := filepath.Join(t.TempDir(), "failed.json")
	if err := WriteAtomic(receiptPath, document); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyV1(receiptPath); err != nil {
		t.Fatalf("failed partial raw evidence was rejected: %v", err)
	}
	evidence.Facts = append(evidence.Facts, RawFact{Name: "requests", Value: 2})
	if err := WriteRawEvidence(filepath.Join(t.TempDir(), "invalid.json"), evidence); err == nil {
		t.Fatal("duplicate raw fact was accepted")
	}
}
