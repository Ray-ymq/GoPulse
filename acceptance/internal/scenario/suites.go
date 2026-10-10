package scenario

import (
	"fmt"
	"strings"

	"github.com/Ray-ymq/GoPulse/acceptance/internal/harness"
)

type suiteSpec struct {
	name        string
	scenario    string
	spec        string
	args        []string
	environment []string
}

var observabilityEnvironment = []string{
	"GOPULSE_BASE_URL",
	"GOPULSE_ADMIN_USERNAME",
	"GOPULSE_USER_USERNAME",
	"GOPULSE_DEMOTION_USERNAME",
	"GOPULSE_ACCEPTANCE_PASSWORD",
	"GOPULSE_USER_ID",
	"GOPULSE_ADMIN_ID",
	"GOPULSE_DEMOTION_ID",
	"GOPULSE_OBSERVABILITY_ADMIN_USERNAME",
	"GOPULSE_OBSERVABILITY_USER_USERNAME",
	"GOPULSE_OBSERVABILITY_PASSWORD",
	"GOPULSE_PLUGIN_ACCOUNT_PASSWORD",
	"GOPULSE_REDIS_PASSWORD",
	"GOPULSE_SCREENSHOT_DIR",
}

// Business runs the production-image browser business matrix in an isolated
// acceptance Compose project. The spec creates its own accounts and therefore
// exercises the clean registration path as well as post, comment, like and
// session recovery.
func Business(root string, options Options) (returnErr error) {
	return runStack(root, options, false, false, []suiteSpec{
		{name: "business", scenario: "business", spec: "e2e/business.spec.ts"},
		{name: "compose-business", scenario: "business", spec: "e2e/compose-business.spec.ts"},
	})
}

// Observe runs one named observability specialist. The default/all scope is
// the same complete setup and admin closure used by `compose`; named scopes
// select an existing checked-in browser matrix so every specialist still
// runs against real Router, Marshaller, Monitor, Kafka, Elasticsearch and
// VictoriaMetrics services.
func Observe(root string, options Options) (returnErr error) {
	scope := strings.TrimSpace(options.Scope)
	if scope == "" {
		scope = "all"
	}
	if scope != "all" && !contains([]string{"exporter", "monitor", "router", "marshaller", "logs", "events"}, scope) {
		return fmt.Errorf("observability scope must be all, exporter, monitor, router, marshaller, logs, or events")
	}
	suite := suiteSpec{name: scope, scenario: "admin", spec: "e2e/compose-observability.spec.ts", environment: observabilityEnvironment}
	if scope == "exporter" {
		suite.spec = "e2e/compose-release-plugins.spec.ts"
	}
	return runStack(root, options, true, scope == "exporter", []suiteSpec{suite})
}

// Plugins covers the six-plugin lifecycle and real metrics/log/event views.
func Plugins(root string, options Options) error {
	return runStack(root, options, true, false, []suiteSpec{{
		name:        "plugins",
		scenario:    "admin",
		spec:        "e2e/admin-frontend.spec.ts",
		args:        []string{"--grep", "existing real metrics logs events"},
		environment: observabilityEnvironment,
	}})
}

// Alerts focuses the existing three-source alert rule lifecycle. The browser
// case uses real scheduler inputs and is kept focused so role and page suites
// can run independently against a fresh project.
func Alerts(root string, options Options) error {
	return runStack(root, options, true, false, []suiteSpec{{
		name:        "alerts",
		scenario:    "admin",
		spec:        "e2e/phase15-closure.spec.ts",
		args:        []string{"--grep", "create exact three-source rules"},
		environment: observabilityEnvironment,
	}})
}

// Roles covers same-origin routing and ordinary-user authorization boundaries.
func Roles(root string, options Options) error {
	return runStack(root, options, true, false, []suiteSpec{{
		name:        "roles",
		scenario:    "admin",
		spec:        "e2e/admin-frontend.spec.ts",
		args:        []string{"--grep", "same-origin paths|ordinary user"},
		environment: observabilityEnvironment,
	}})
}

// Pages runs the data-backed management page checks. Layout screenshot output
// is intentionally disabled for the acceptance command; visual artifacts are
// owned by the existing frontend visual job and are not a private receipt.
func Pages(root string, options Options) error {
	return runStack(root, options, true, false, []suiteSpec{
		{
			name:        "pages-data",
			scenario:    "admin",
			spec:        "e2e/compose-observability.spec.ts",
			environment: observabilityEnvironment,
		},
		{
			name:        "pages-alerts",
			scenario:    "admin",
			spec:        "e2e/dashboard.spec.ts",
			args:        []string{"--grep", "real overview, catalog rule lifecycle"},
			environment: observabilityEnvironment,
		},
		{
			name:        "pages",
			scenario:    "admin",
			spec:        "e2e/admin-visual.spec.ts",
			environment: observabilityEnvironment,
		},
	})
}

// ReconcileAccounts keeps the account preparation operation available as a
// native subcommand. It is deliberately idempotent: the setup browser flow
// creates the two acceptance accounts and the role bootstrap promotes the
// generated administrator exactly once.
func ReconcileAccounts(root string, options Options) error {
	return runStack(root, options, true, true, nil)
}

func runStack(root string, options Options, setup, reconcile bool, suites []suiteSpec) (returnErr error) {
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
	buildTargets := []string{"backend", "business-worker", "search-indexer", "admin-frontend", "frontend", "acceptance", "router", "marshaller", "monitor", "redis-exporter"}
	if options.Manifest != "" {
		if err := configureCandidate(session, resolvePath(root, options.Manifest)); err != nil {
			session.SetStatus("failed", err)
			return err
		}
		buildTargets = []string{"acceptance"}
	}
	build := session.Compose(append([]string{"build"}, buildTargets...)...)
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
	smoke := session.AcceptanceRun("", "e2e/compose-smoke.spec.ts")
	if smoke.ExitCode != 0 {
		err := fmt.Errorf("Compose smoke acceptance failed with exit code %d", smoke.ExitCode)
		session.SetStatus("failed", err)
		return err
	}
	if setup {
		if result := session.AcceptanceRun("setup", "e2e/compose-observability.spec.ts", "GOPULSE_OBSERVABILITY_ADMIN_USERNAME", "GOPULSE_OBSERVABILITY_USER_USERNAME", "GOPULSE_OBSERVABILITY_PASSWORD"); result.ExitCode != 0 {
			err := fmt.Errorf("observability account setup failed with exit code %d", result.ExitCode)
			session.SetStatus("failed", err)
			return err
		}
		if err := promoteAdmin(session); err != nil {
			session.SetStatus("failed", err)
			return err
		}
		if reconcile {
			if err := reconcilePluginAccounts(session); err != nil {
				session.SetStatus("failed", err)
				return err
			}
		}
	}
	for _, suite := range suites {
		result := session.AcceptanceRunArgs(suite.scenario, suite.spec, suite.args, suite.environment...)
		if result.ExitCode != 0 {
			err := fmt.Errorf("acceptance %s suite failed with exit code %d", suite.name, result.ExitCode)
			session.SetStatus("failed", err)
			return err
		}
	}
	session.SetStatus("passed", nil)
	return nil
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
