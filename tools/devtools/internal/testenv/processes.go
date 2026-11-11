package testenv

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Ray-ymq/GoPulse/devtools/internal/proc"
	"github.com/Ray-ymq/GoPulse/devtools/internal/workspace"
)

const (
// registers and then removes again.
)

// runIntegrationTests executes the scope-specific test command. The caller marks
// the lifecycle failed and still cleans up when this returns an error.
func (s *session) runIntegrationTests(state *workspace.State, scope string, observe bool) error {
	oneShot := s.testEnvironment()
	if err := s.runCommand([]string{"go", "run", "./cmd/migrate", "up"},
		filepath.Join(s.root, "backend/business"), oneShot, "test database migration", 0); err != nil {
		return err
	}
	if err := s.runCommand([]string{"go", "run", "./cmd/search-reindex", "--if-missing"},
		filepath.Join(s.root, "backend/business"), oneShot, "test search reindex", 0); err != nil {
		return err
	}
	environment := s.testEnvironment()
	return s.runCommand([]string{"go", "-C", "backend/business", "test", "-p", "1", "-tags=integration", "./..."},
		s.root, environment, "business integration tests", businessIntegrationTimeout)
}

// testEnvironment is the owned child environment plus the integration flag.
func (s *session) testEnvironment() map[string]string {
	environment := copyEnvironment(s.values)
	for key, value := range s.processEnv {
		if _, known := environment[key]; !known {
			environment[key] = value
		}
	}
	environment["INTEGRATION_TESTS"] = "1"
	return environment
}

// startObservationProcesses starts only the source services the native
// observation flow needs, then registers and promotes its test administrator.

// registerObservationAdmin creates the deterministic administrator and promotes
// it through the real command the observation flow expects.

// cleanup terminates the owned test processes and removes only the accounts the
// observation flow created.
func (s *session) cleanup(state *workspace.State, observe bool) []error {
	cleanupErrors := []error{}
	for _, record := range state.Processes {
		proc.Terminate(record)
	}

	if s.stopDependenciesResult != nil {
		if err := s.stopDependenciesResult(state); err != nil {
			cleanupErrors = append(cleanupErrors, err)
			fmt.Fprintf(os.Stderr, "[gopulse] warning: %v\n", err)
		}
	} else if s.stopDependencies != nil {
		s.stopDependencies(state)
	}

	state.StoppedAt = now()
	if err := s.saveIntegrationState(state); err != nil {
		cleanupErrors = append(cleanupErrors, err)
		fmt.Fprintf(os.Stderr, "[gopulse] warning: %v\n", err)
	}
	return cleanupErrors
}

// observationUsernames is the deterministic set the integration flow may
// create for this workspace.

// cleanupObservationAccounts deletes the related rows and then the test accounts
// themselves, in the order the replaced helper used.

// buildBinary reuses a previously built binary while its source digest matches.
