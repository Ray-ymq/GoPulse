// Package scenario contains the user-facing acceptance command scenarios.
package scenario

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Ray-ymq/GoPulse/acceptance/internal/contracts"
	"github.com/Ray-ymq/GoPulse/acceptance/internal/harness"
)

type Options struct {
	Scope     string
	Keep      bool
	SelfTest  bool
	Receipt   string
	Manifest  string
	Candidate string
	Install   string
	Platform  string
}

func Compose(root string, options Options) (returnErr error) {
	if options.SelfTest {
		return selfTest()
	}
	if options.Scope != "" && options.Scope != "observability" && options.Scope != "business" {
		return fmt.Errorf("scope must be business or observability")
	}
	if options.Manifest != "" {
		if options.Scope == "business" {
			return runStack(root, options, false, false, []suiteSpec{
				{name: "business", scenario: "business", spec: "e2e/compose-business.spec.ts"},
			})
		}
		return runStack(root, options, true, false, []suiteSpec{
			{name: "observability", scenario: "admin", spec: "e2e/compose-observability.spec.ts", environment: observabilityEnvironment},
		})
	}
	session, err := harness.New(root, options.Keep)
	if err != nil {
		return err
	}
	defer func() {
		cleanupErr := session.Cleanup()
		if returnErr == nil && cleanupErr != nil {
			returnErr = cleanupErr
		}
		if options.Receipt != "" {
			if receiptErr := session.WriteReceipt(resolvePath(root, options.Receipt)); returnErr == nil && receiptErr != nil {
				returnErr = receiptErr
			}
		}
	}()
	if err := session.Preflight(); err != nil {
		session.SetStatus("failed", err)
		return err
	}
	session.MarkStarted()
	build := session.Compose("build", "backend", "business-worker", "search-indexer", "admin-frontend", "frontend", "acceptance", "router", "marshaller", "monitor", "redis-exporter")
	if build.ExitCode != 0 {
		err := fmt.Errorf("acceptance image build failed with exit code %d", build.ExitCode)
		session.SetStatus("failed", err)
		return err
	}
	up := session.Compose("up", "--detach", "--wait", "--wait-timeout", "420")
	if up.ExitCode != 0 {
		err := fmt.Errorf("Compose acceptance startup failed with exit code %d", up.ExitCode)
		session.SetStatus("failed", err)
		return err
	}
	smoke := session.Compose("--profile", "acceptance", "run", "--rm", "--no-deps", "acceptance", "e2e/compose-smoke.spec.ts")
	if smoke.ExitCode != 0 {
		err := fmt.Errorf("Compose smoke acceptance failed with exit code %d", smoke.ExitCode)
		session.SetStatus("failed", err)
		return err
	}
	if options.Scope == "" || options.Scope == "business" {
		if err := runSpec(session, "business", "e2e/compose-business.spec.ts"); err != nil {
			session.SetStatus("failed", err)
			return err
		}
	}
	if options.Scope == "" || options.Scope == "observability" {
		if err := runSpec(session, "setup", "e2e/compose-observability.spec.ts"); err != nil {
			session.SetStatus("failed", err)
			return err
		}
		if err := promoteAdmin(session); err != nil {
			session.SetStatus("failed", err)
			return err
		}
		if err := runSpec(session, "admin", "e2e/compose-observability.spec.ts"); err != nil {
			session.SetStatus("failed", err)
			return err
		}
	}
	session.SetStatus("passed", nil)
	return nil
}

func promoteAdmin(session *harness.Session) error {
	username := "admin_" + session.Token
	query := "MYSQL_PWD=\"$MYSQL_PASSWORD\" mysql --user=\"$MYSQL_USER\" --batch --skip-column-names \"$MYSQL_DATABASE\" --execute \"SELECT id FROM users WHERE username='" + username + "'\""
	result := session.Compose("exec", "-T", "mysql", "sh", "-ec", query)
	if result.ExitCode != 0 {
		return fmt.Errorf("query acceptance administrator failed with exit code %d", result.ExitCode)
	}
	userID := strings.TrimSpace(result.Stdout)
	if !positiveInteger(userID) {
		return fmt.Errorf("acceptance administrator ID is invalid: %q", userID)
	}
	userQuery := "MYSQL_PWD=\"$MYSQL_PASSWORD\" mysql --user=\"$MYSQL_USER\" --batch --skip-column-names \"$MYSQL_DATABASE\" --execute \"SELECT id FROM users WHERE username='user_" + session.Token + "'\""
	userResult := session.Compose("exec", "-T", "mysql", "sh", "-ec", userQuery)
	if userResult.ExitCode != 0 {
		return fmt.Errorf("query acceptance user failed with exit code %d", userResult.ExitCode)
	}
	ordinaryID := strings.TrimSpace(userResult.Stdout)
	if !positiveInteger(ordinaryID) {
		return fmt.Errorf("acceptance user ID is invalid: %q", ordinaryID)
	}
	session.SetValue("GOPULSE_ADMIN_ID", userID)
	session.SetValue("GOPULSE_USER_ID", ordinaryID)
	session.SetValue("GOPULSE_DEMOTION_ID", ordinaryID)
	bootstrap := session.Compose("--profile", "operations", "run", "--rm", "--no-deps", "admin-role", "bootstrap", "--user-id", userID)
	if bootstrap.ExitCode != 0 {
		return fmt.Errorf("bootstrap acceptance administrator failed with exit code %d", bootstrap.ExitCode)
	}
	return nil
}

func positiveInteger(value string) bool {
	if value == "" || value == "0" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func runSpec(session *harness.Session, scenario, spec string) error {
	result := session.AcceptanceRun(scenario, spec, "GOPULSE_OBSERVABILITY_ADMIN_USERNAME", "GOPULSE_OBSERVABILITY_USER_USERNAME", "GOPULSE_OBSERVABILITY_PASSWORD")
	if result.ExitCode != 0 {
		return fmt.Errorf("Compose acceptance %s failed with exit code %d", scenario, result.ExitCode)
	}
	return nil
}

func selfTest() error {
	if !contracts.ValidProjectName("gopulse-accept-012345abcdef") {
		return fmt.Errorf("valid acceptance project was rejected")
	}
	for _, candidate := range []string{"", "gopulse", "gopulse-accept-123", "../gopulse", "gopulse-accept-012345abcdeg", "GOPULSE-accept-012345abcdef"} {
		if contracts.ValidProjectName(candidate) {
			return fmt.Errorf("unsafe acceptance project was accepted: %s", candidate)
		}
	}
	for _, host := range []string{"0.0.0.0", "localhost"} {
		if contracts.ValidLoopback(host) {
			return fmt.Errorf("non-loopback host was accepted: %s", host)
		}
	}
	fmt.Println("[gopulse-acceptance] PASS: project ownership and loopback publication contracts")
	return nil
}

func resolvePath(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

func Unsupported(name string) error {
	return fmt.Errorf("acceptance scenario %s is not implemented", strings.TrimSpace(name))
}
