package devrun

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/devtools/internal/proc"
	"github.com/Ray-ymq/GoPulse/devtools/internal/workspace"
)

const exampleEnvironment = "HTTP_PORT=8080\nFRONTEND_PORT=5173\nADMIN_FRONTEND_PORT=5174\nMYSQL_PORT=3306\n"

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env.example"), []byte(exampleEnvironment), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte("2.5.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func newTestSession(t *testing.T, root string, observe bool) *session {
	t.Helper()
	mode := "dev"
	if observe {
		mode = "observe"
	}
	current, err := newSession(root, mode, observe, "")
	if err != nil {
		t.Fatal(err)
	}
	return current
}

func runningState(current *session, stage string, observe bool, digests request) *workspace.State {
	state := &workspace.State{
		Schema:            workspace.Schema,
		Status:            "running",
		WorkspaceRoot:     current.ws.Root,
		WorkspaceID:       current.ws.Identity,
		Mode:              digests.mode,
		Stage:             stage,
		Observe:           observe,
		Project:           current.project,
		SourceDigest:      digests.source,
		ComposeDigest:     digests.compose,
		EnvironmentDigest: digests.environment,
		Processes:         map[string]workspace.ProcessRecord{},
	}
	if stage != workspace.StageDeps {
		// A running application lifecycle always owns live processes.
		state.Processes["backend"] = workspace.ProcessRecord{PID: os.Getpid(), Birth: birthOfSelf()}
	}
	return state
}

func birthOfSelf() string {
	// The test process itself is a live owned process, which keeps ownership
	// checks deterministic without spawning anything.
	birth, _ := proc.BirthIdentity(os.Getpid())
	return birth
}

func TestDependencyStageCanBeContinuedByBothApplicationStages(t *testing.T) {
	for _, observe := range []bool{false, true} {
		root := fixture(t)
		current := newTestSession(t, root, observe)
		requested := request{
			stage:       stageFor(observe),
			mode:        modeFor(observe),
			observe:     observe,
			source:      "source",
			compose:     "compose",
			environment: "environment",
		}
		dependencies := runningState(newTestSession(t, root, false), workspace.StageDeps, false, request{
			environment: "environment",
		})
		if action, err := resolveExisting(current, dependencies, requested); err != nil || action != actionStart {
			t.Fatalf("observe=%v: a dependency-only stage must be continued (action=%v err=%v)", observe, action, err)
		}
	}
}

func TestDependencyStageOfObservationScopeRejectsPlainDevelopment(t *testing.T) {
	root := fixture(t)
	current := newTestSession(t, root, false)
	requested := request{stage: workspace.StageDev, mode: "dev", observe: false,
		source: "source", compose: "compose", environment: "environment"}
	dependencies := runningState(newTestSession(t, root, true), workspace.StageDeps, true, request{environment: "environment"})
	_, err := resolveExisting(current, dependencies, requested)
	if err == nil || !strings.Contains(err.Error(), "run make stop first") {
		t.Fatalf("an observation dependency stage must not be silently narrowed: %v", err)
	}
}

func TestApplicationStagesDoNotReplaceEachOther(t *testing.T) {
	root := fixture(t)
	requested := request{stage: workspace.StageDevObserve, mode: "observe", observe: true,
		source: "source", compose: "compose", environment: "environment"}
	development := runningState(newTestSession(t, root, false), workspace.StageDev, false, requested)
	_, err := resolveExisting(newTestSession(t, root, true), development, requested)
	if err == nil || !strings.Contains(err.Error(), "already owns an active dev lifecycle") {
		t.Fatalf("a running dev lifecycle must refuse dev-observe: %v", err)
	}
}

func TestRepeatedStartOfSameStageIsIdempotent(t *testing.T) {
	root := fixture(t)
	current := newTestSession(t, root, false)
	requested := request{stage: workspace.StageDev, mode: "dev", observe: false,
		source: "source", compose: "compose", environment: "environment"}
	state := runningState(current, workspace.StageDev, false, requested)
	action, err := resolveExisting(current, state, requested)
	if err != nil || action != actionAlreadyRunning {
		t.Fatalf("a repeated start must be idempotent (action=%v err=%v)", action, err)
	}
}

func TestChangedInputsAndStaleProcessesRefuseRestart(t *testing.T) {
	root := fixture(t)
	current := newTestSession(t, root, false)
	requested := request{stage: workspace.StageDev, mode: "dev", observe: false,
		source: "source", compose: "compose", environment: "environment"}

	changed := runningState(current, workspace.StageDev, false, requested)
	changed.SourceDigest = "other"
	if _, err := resolveExisting(current, changed, requested); err == nil ||
		!strings.Contains(err.Error(), "inputs changed") {
		t.Fatalf("changed inputs must refuse a restart: %v", err)
	}

	stale := runningState(current, workspace.StageDev, false, requested)
	stale.Processes["backend"] = workspace.ProcessRecord{PID: 0, Birth: "gone"}
	if _, err := resolveExisting(current, stale, requested); err == nil ||
		!strings.Contains(err.Error(), "stale") {
		t.Fatalf("a stale process record must refuse a restart: %v", err)
	}
}

func TestStateWrittenByTheReplacedHelperIsUnderstood(t *testing.T) {
	root := fixture(t)
	current := newTestSession(t, root, false)
	// The replaced helper recorded no stage field.
	legacy := runningState(current, "", false, request{
		mode: "dev", source: "source", compose: "compose", environment: "environment",
	})
	legacy.Stage = ""
	requested := request{stage: workspace.StageDev, mode: "dev", observe: false,
		source: "source", compose: "compose", environment: "environment"}
	action, err := resolveExisting(current, legacy, requested)
	if err != nil || action != actionAlreadyRunning {
		t.Fatalf("a legacy dev state must be recognised as the same stage (action=%v err=%v)", action, err)
	}

	legacyObservation := runningState(newTestSession(t, root, true), "", true, request{
		mode: "observe", source: "source", compose: "compose", environment: "environment",
	})
	legacyObservation.Stage = ""
	observationRequest := request{stage: workspace.StageDevObserve, mode: "observe", observe: true,
		source: "source", compose: "compose", environment: "environment"}
	if action, err := resolveExisting(newTestSession(t, root, true), legacyObservation, observationRequest); err != nil || action != actionAlreadyRunning {
		t.Fatalf("a legacy observation state must be recognised (action=%v err=%v)", action, err)
	}
}

func TestStoppedStateAllowsFreshStart(t *testing.T) {
	root := fixture(t)
	current := newTestSession(t, root, false)
	state := runningState(current, workspace.StageDev, false, request{
		mode: "dev", source: "source", compose: "compose", environment: "environment",
	})
	state.Status = "stopped"
	action, err := resolveExisting(current, state, request{stage: workspace.StageDev, mode: "dev", observe: false})
	if err != nil || action != actionStart {
		t.Fatalf("a stopped lifecycle must allow a fresh start (action=%v err=%v)", action, err)
	}
}

func TestDependencyStartReportsRunningDependencies(t *testing.T) {
	root := fixture(t)
	current := newTestSession(t, root, false)
	requested := request{stage: workspace.StageDeps, mode: "dev", observe: false,
		source: "source", compose: "compose", environment: "environment"}
	development := runningState(current, workspace.StageDev, false, requested)
	action, err := resolveExisting(current, development, requested)
	if err != nil || action != actionAlreadyRunning {
		t.Fatalf("dependencies of a running lifecycle are already up (action=%v err=%v)", action, err)
	}
}

func TestUnknownScopeIsRejected(t *testing.T) {
	if err := Deps(fixture(t), "staging", ""); err == nil || !strings.Contains(err.Error(), "scope must be") {
		t.Fatalf("an unknown scope must be refused: %v", err)
	}
}

func modeFor(observe bool) string {
	if observe {
		return "observe"
	}
	return "dev"
}

func stageFor(observe bool) string {
	if observe {
		return workspace.StageDevObserve
	}
	return workspace.StageDev
}
