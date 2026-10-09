package testenv

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/devtools/internal/digest"
	"github.com/Ray-ymq/GoPulse/devtools/internal/proc"
	"github.com/Ray-ymq/GoPulse/devtools/internal/ready"
	"github.com/Ray-ymq/GoPulse/devtools/internal/workspace"
)

const (
	browserTimeout       = 600 * time.Second
	browserAdminPassword = "observe-browser-password-32-bytes-0123456789"
)

// E2E runs one isolated browser scope: its own Compose project, test accounts,
// source services, two Vite processes, and Playwright commands.
func E2E(root, scope string, overrides map[string]string) error {
	observe, err := scopeFor(scope, "e2e")
	if err != nil {
		return err
	}
	current, err := newSession(root, observe, overrides)
	if err != nil {
		return err
	}
	lifecycle, err := workspace.Load(current.ws)
	if err != nil {
		return err
	}
	if lifecycle != nil && lifecycle.Status == "running" {
		return fmt.Errorf("cannot start browser checks while this workspace owns a development lifecycle; run make stop first")
	}
	return runE2E(current, scope, observe)
}

// runE2E owns the locked browser lifecycle. The trace directory and the browser
// commands are recorded in the same state document the integration scopes use.
func runE2E(current *session, scope string, observe bool) error {
	release, err := current.lock()
	if err != nil {
		return err
	}
	defer release()

	if err := proc.CheckPorts(e2ePorts(current.values, observe), nil); err != nil {
		return err
	}
	composeEnv := copyEnvironment(current.processEnv)
	if observe {
		image, err := current.prepareMonitor(current)
		if err != nil {
			return err
		}
		composeEnv["GOPULSE_MONITOR_IMAGE"] = image
	}
	if err := current.startDependencies(composeEnv); err != nil {
		current.stopDependencies(current.pendingState())
		return err
	}
	state, err := current.e2eState(scope, observe, composeEnv["GOPULSE_MONITOR_IMAGE"])
	if err != nil {
		current.stopDependencies(current.pendingState())
		return err
	}
	if err := current.saveIntegrationState(state); err != nil {
		current.stopDependencies(state)
		return err
	}

	runErr := current.runBrowserChecks(state, scope, observe)
	if runErr != nil {
		state.Status = "failed"
	} else {
		state.Status = "passed"
		fmt.Printf("[gopulse] e2e scope=%s passed for test project %s\n", scope, current.project)
	}
	current.cleanup(state, observe)
	return runErr
}

// e2ePorts is the browser scope port set: the integration set plus both Vite
// listeners and the three runtime contract ports.
func e2ePorts(values map[string]string, observe bool) []int {
	ports := integrationPorts(values, observe)
	ports = append(ports, integer(values, "HTTP_PORT"), integer(values, "FRONTEND_PORT"), 19101, 19102, 19103)
	if observe {
		ports = append(ports, integer(values, "ADMIN_FRONTEND_PORT"))
	}
	return ports
}

func (s *session) e2eState(scope string, observe bool, image string) (*workspace.State, error) {
	state, err := s.integrationState(scope, observe, image)
	if err != nil {
		return nil, err
	}
	state.Mode = "e2e"
	state.SourceDigest, err = digest.E2ESource(s.root, observe)
	if err != nil {
		return nil, err
	}
	state.BrowserTraces = filepath.Join(s.root, "test-results")
	return state, nil
}

// runBrowserChecks starts the scope processes and runs the Playwright commands.
func (s *session) runBrowserChecks(state *workspace.State, scope string, observe bool) error {
	if err := s.startBrowserProcesses(state, observe); err != nil {
		return err
	}
	environment := copyEnvironment(s.processEnv)
	environment["GOPULSE_BASE_URL"] = fmt.Sprintf("http://127.0.0.1:%s", s.values["FRONTEND_PORT"])
	environment["GOPULSE_ACCEPTANCE_TOKEN"] = fmt.Sprintf("%s%d", s.ws.Identity, time.Now().Unix())
	frontend := filepath.Join(s.root, "frontend")
	commands := [][]string{}
	labels := []string{}
	if observe {
		for key, source := range map[string]string{
			"GOPULSE_ADMIN_USERNAME":               "OBSERVE_ADMIN_USERNAME",
			"GOPULSE_USER_USERNAME":                "OBSERVE_USER_USERNAME",
			"GOPULSE_DEMOTION_USERNAME":            "OBSERVE_DEMOTION_USERNAME",
			"GOPULSE_ACCEPTANCE_PASSWORD":          "OBSERVE_ADMIN_PASSWORD",
			"GOPULSE_OBSERVABILITY_ADMIN_USERNAME": "OBSERVE_ADMIN_USERNAME",
			"GOPULSE_OBSERVABILITY_USER_USERNAME":  "OBSERVE_USER_USERNAME",
			"GOPULSE_OBSERVABILITY_PASSWORD":       "OBSERVE_ADMIN_PASSWORD",
			"GOPULSE_REDIS_PASSWORD":               "REDIS_PASSWORD",
		} {
			environment[key] = s.values[source]
		}
		admin := []string{"npm", "exec", "--", "playwright", "test", "e2e/admin-frontend.spec.ts",
			"--grep", "same-origin paths|ordinary user|database demotion"}
		observability := []string{"npm", "exec", "--", "playwright", "test", "e2e/compose-observability.spec.ts"}
		commands = append(commands, admin, observability)
		labels = append(labels, "native admin browser checks", "native observability browser checks")
	} else {
		business := []string{"npm", "exec", "--", "playwright", "test", "e2e/business.spec.ts",
			"e2e/compose-business.spec.ts", "--grep-invert", "search-rebuild|search-live"}
		commands = append(commands, business)
		labels = append(labels, "native business browser checks")
	}
	state.BrowserCommands = commands
	if err := s.saveIntegrationState(state); err != nil {
		return err
	}
	for index, command := range commands {
		scenario := "business"
		if observe {
			scenario = "admin"
		}
		current := copyEnvironment(environment)
		current["GOPULSE_ACCEPTANCE_SCENARIO"] = scenario
		if err := proc.Run(command, frontend, current, labels[index], browserTimeout); err != nil {
			return err
		}
	}
	return nil
}

// startBrowserProcesses starts the source services and both Vite processes the
// browser scopes need, in the recorded order.
func (s *session) startBrowserProcesses(state *workspace.State, observe bool) error {
	oneShot := s.testEnvironment()
	oneShot["LOG_MONITOR_URL"] = ""
	oneShot["LOG_MONITOR_INGEST_TOKEN"] = ""
	backendDir := filepath.Join(s.root, "backend")
	if err := proc.Run([]string{"go", "run", "./cmd/migrate", "up"}, backendDir, oneShot,
		"test database migration", 0); err != nil {
		return err
	}
	if err := proc.Run([]string{"go", "run", "./cmd/search-reindex", "--if-missing"}, backendDir, oneShot,
		"test search reindex", 0); err != nil {
		return err
	}
	buildDigest, err := digest.SourceBuilds(s.root)
	if err != nil {
		return err
	}
	backend, err := s.buildBinary("e2e-backend", backendDir, "./cmd/server", buildDigest)
	if err != nil {
		return err
	}
	worker, indexer := "", ""
	if !observe {
		if worker, err = s.buildBinary("e2e-business-worker", backendDir, "./cmd/business-worker", buildDigest); err != nil {
			return err
		}
		if indexer, err = s.buildBinary("e2e-search-indexer", backendDir, "./cmd/search-indexer", buildDigest); err != nil {
			return err
		}
	}
	if observe {
		observationDigest, err := digest.Source(s.root, true)
		if err != nil {
			return err
		}
		router, err := s.buildBinary("e2e-router", filepath.Join(s.root, "router"), "./cmd/router", observationDigest)
		if err != nil {
			return err
		}
		marshaller, err := s.buildBinary("e2e-marshaller", filepath.Join(s.root, "marshaller"), "./cmd/marshaller", observationDigest)
		if err != nil {
			return err
		}
		if err := s.spawn(state, "router", []string{router},
			instance(s.testEnvironment(), "router-e2e"), filepath.Join(s.root, "router")); err != nil {
			return err
		}
		if err := ready.Wait(fmt.Sprintf("http://127.0.0.1:%s/ready", s.values["ROUTER_HTTP_PORT"]), readinessTimeout,
			s.values["ROUTER_API_TOKEN"], "test Router", aliveIn(state, "router")); err != nil {
			return err
		}
		if err := s.spawn(state, "marshaller", []string{marshaller},
			instance(s.testEnvironment(), "marshaller-e2e"), filepath.Join(s.root, "marshaller")); err != nil {
			return err
		}
		if err := ready.Wait(fmt.Sprintf("http://127.0.0.1:%s/ready", s.values["MARSHALLER_HTTP_PORT"]), readinessTimeout,
			s.values["MARSHALLER_API_TOKEN"], "test Marshaller", aliveIn(state, "marshaller")); err != nil {
			return err
		}
	}
	if err := s.spawn(state, "backend", []string{backend},
		instance(s.testEnvironment(), "backend-e2e"), backendDir); err != nil {
		return err
	}
	if err := ready.Wait(fmt.Sprintf("http://127.0.0.1:%s/ready", s.values["HTTP_PORT"]), readinessTimeout,
		"", "test Backend", aliveIn(state, "backend")); err != nil {
		return err
	}
	if !observe {
		if err := s.spawn(state, "business-worker", []string{worker},
			instance(s.testEnvironment(), "business-worker-e2e"), backendDir); err != nil {
			return err
		}
		if err := ready.Wait("http://127.0.0.1:19102/ready", readinessTimeout, "",
			"test Business Worker", aliveIn(state, "business-worker")); err != nil {
			return err
		}
		if err := s.spawn(state, "search-indexer", []string{indexer},
			instance(s.testEnvironment(), "search-indexer-e2e"), backendDir); err != nil {
			return err
		}
		if err := ready.Wait("http://127.0.0.1:19103/ready", readinessTimeout, "",
			"test Search Indexer", aliveIn(state, "search-indexer")); err != nil {
			return err
		}
	} else if err := ready.Wait(fmt.Sprintf("http://127.0.0.1:%s/ready", s.values["MONITOR_HTTP_PORT"]),
		readinessTimeout, s.values["MONITOR_API_TOKEN"], "test Monitor", nil); err != nil {
		return err
	}

	frontendDir := filepath.Join(s.root, "frontend")
	if err := s.ensureNpmDependencies("frontend"); err != nil {
		return err
	}
	if err := s.spawn(state, "frontend", []string{"npm", "run", "dev", "--", "--host", "127.0.0.1",
		"--port", s.values["FRONTEND_PORT"]}, s.testEnvironment(), frontendDir); err != nil {
		return err
	}
	if err := ready.Wait(fmt.Sprintf("http://127.0.0.1:%s/", s.values["FRONTEND_PORT"]), readinessTimeout,
		"", "test user Vite", aliveIn(state, "frontend")); err != nil {
		return err
	}
	if observe {
		adminDir := filepath.Join(s.root, "admin-frontend")
		if err := s.ensureNpmDependencies("admin-frontend"); err != nil {
			return err
		}
		adminEnv := s.testEnvironment()
		adminEnv["FRONTEND_PORT"] = s.values["ADMIN_FRONTEND_PORT"]
		if err := s.spawn(state, "admin-frontend", []string{"npm", "run", "dev", "--", "--host", "127.0.0.1",
			"--port", s.values["ADMIN_FRONTEND_PORT"]}, adminEnv, adminDir); err != nil {
			return err
		}
		if err := ready.Wait(fmt.Sprintf("http://127.0.0.1:%s/admin/", s.values["ADMIN_FRONTEND_PORT"]), readinessTimeout,
			"", "test admin Vite", aliveIn(state, "admin-frontend")); err != nil {
			return err
		}
		if err := s.registerBrowserAccounts(buildDigest); err != nil {
			return err
		}
	}
	return nil
}

// registerBrowserAccounts creates the three deterministic browser accounts and
// prepares the administrator and demotion roles.
func (s *session) registerBrowserAccounts(buildDigest string) error {
	admin := fmt.Sprintf("observe_admin_%s", s.ws.Identity)
	user := fmt.Sprintf("observe_user_%s", s.ws.Identity)
	demotion := fmt.Sprintf("observe_demote_%s", s.ws.Identity)
	s.values["OBSERVE_ADMIN_USERNAME"] = admin
	s.values["OBSERVE_USER_USERNAME"] = user
	s.values["OBSERVE_DEMOTION_USERNAME"] = demotion
	s.values["OBSERVE_ADMIN_PASSWORD"] = browserAdminPassword
	for _, username := range []string{admin, user, demotion} {
		if err := s.registerAccount(username, browserAdminPassword); err != nil {
			return err
		}
	}
	adminRole, err := s.buildBinary("e2e-admin-role", filepath.Join(s.root, "backend"), "./cmd/admin-role", buildDigest)
	if err != nil {
		return err
	}
	if err := proc.Run([]string{adminRole, "promote", "--username", admin},
		filepath.Join(s.root, "backend"), s.testEnvironment(), "test browser admin-role bootstrap", 0); err != nil {
		return err
	}
	return s.promoteAccountViaAPI(admin, demotion, browserAdminPassword)
}

// registerAccount creates one deterministic test account through the public API.
func (s *session) registerAccount(username, password string) error {
	payload, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		return err
	}
	command := []string{"curl", "--silent", "--show-error", "--max-time", "20",
		"-H", "Content-Type: application/json", "-d", string(payload), "-o", os.DevNull, "-w", "%{http_code}",
		fmt.Sprintf("http://127.0.0.1:%s/api/v1/auth/register", s.values["HTTP_PORT"])}
	status, err := proc.Capture(command, s.root, s.testEnvironment())
	if err != nil {
		return fmt.Errorf("test account registration for %s failed", username)
	}
	switch strings.TrimSpace(status) {
	case "201", "409":
		return nil
	default:
		return fmt.Errorf("test account registration for %s failed", username)
	}
}

// promoteAccountViaAPI gives the demotion account the role the browser scenario
// expects, using the administrator session the same way the checks do.
func (s *session) promoteAccountViaAPI(admin, target, password string) error {
	targetID, err := s.accountID(target, password)
	if err != nil {
		return err
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 20 * time.Second, Jar: jar, Transport: &http.Transport{Proxy: nil}}
	if _, err := s.apiJSON(client, "/api/v1/auth/login", http.MethodPost,
		map[string]any{"username": admin, "password": password}, "test browser administrator login"); err != nil {
		return err
	}
	response, err := s.apiJSON(client, fmt.Sprintf("/api/v1/admin/users/%d/role", targetID), http.MethodPut,
		map[string]any{"role": "super_admin"}, "test browser demotion-role setup")
	if err != nil {
		return err
	}
	data, _ := response["data"].(map[string]any)
	user, _ := data["user"].(map[string]any)
	if user["role"] != "super_admin" {
		return fmt.Errorf("test browser demotion-role setup returned an invalid role")
	}
	return nil
}

// accountID resolves one account identifier through a fresh login session.
func (s *session) accountID(username, password string) (int, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return 0, err
	}
	client := &http.Client{Timeout: 20 * time.Second, Jar: jar, Transport: &http.Transport{Proxy: nil}}
	if _, err := s.apiJSON(client, "/api/v1/auth/login", http.MethodPost,
		map[string]any{"username": username, "password": password},
		fmt.Sprintf("test account login for %s", username)); err != nil {
		return 0, err
	}
	response, err := s.apiJSON(client, "/api/v1/users/me", http.MethodGet, nil,
		fmt.Sprintf("test account identity for %s", username))
	if err != nil {
		return 0, err
	}
	data, _ := response["data"].(map[string]any)
	identifier, ok := data["id"].(float64)
	if !ok || identifier <= 0 {
		return 0, fmt.Errorf("test account identity for %s returned an invalid user", username)
	}
	return int(identifier), nil
}

// apiJSON performs one JSON request without any proxy configuration and returns
// the decoded envelope.
func (s *session) apiJSON(client *http.Client, path, method string, payload map[string]any, label string) (map[string]any, error) {
	body := ""
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = string(encoded)
	}
	address := fmt.Sprintf("http://127.0.0.1:%s%s", s.values["HTTP_PORT"], path)
	request, err := http.NewRequest(method, address, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w", label, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%s returned HTTP %d", label, response.StatusCode)
	}
	decoded := map[string]any{}
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("%s returned an invalid document: %w", label, err)
	}
	return decoded, nil
}

func (s *session) ensureNpmDependencies(directory string) error {
	target := filepath.Join(s.root, directory)
	if info, err := os.Stat(filepath.Join(target, "node_modules")); err == nil && info.IsDir() {
		return nil
	}
	return proc.Run([]string{"npm", "ci", "--no-audit", "--no-fund"}, target, s.processEnv,
		"npm dependency installation for "+directory, 0)
}

func instance(environment map[string]string, identifier string) map[string]string {
	values := copyEnvironment(environment)
	values["GOPULSE_INSTANCE_ID"] = identifier
	return values
}
