package devrun

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Ray-ymq/GoPulse/devtools/internal/digest"
	"github.com/Ray-ymq/GoPulse/devtools/internal/proc"
	"github.com/Ray-ymq/GoPulse/devtools/internal/ready"
	"github.com/Ray-ymq/GoPulse/devtools/internal/workspace"
)

// startApplications prepares the database, builds the source binaries, and
// starts every owned source process in the order the replaced helper used.
func (s *session) startApplications(state *workspace.State, observe bool, env environment) error {
	oneShot := copyEnvironment(s.processEnv)
	// One-shot preparation must not ship its logs to the Monitor.
	oneShot["LOG_MONITOR_URL"] = ""
	oneShot["LOG_MONITOR_INGEST_TOKEN"] = ""
	backendDir := filepath.Join(s.root, "backend")
	if err := proc.Run([]string{"go", "run", "./cmd/migrate", "up"}, backendDir, oneShot, "database migration", 0); err != nil {
		return err
	}
	if err := proc.Run([]string{"go", "run", "./cmd/search-reindex", "--if-missing"}, backendDir, oneShot, "search reindex", 0); err != nil {
		return err
	}

	binaryDigest, err := digest.SourceBuilds(s.root)
	if err != nil {
		return err
	}
	backend, err := s.buildBinary("backend", backendDir, "./cmd/server", binaryDigest)
	if err != nil {
		return err
	}
	worker, err := s.buildBinary("business-worker", backendDir, "./cmd/business-worker", binaryDigest)
	if err != nil {
		return err
	}
	indexer, err := s.buildBinary("search-indexer", backendDir, "./cmd/search-indexer", binaryDigest)
	if err != nil {
		return err
	}

	if observe {
		observationDigest, err := digest.Source(s.root, true)
		if err != nil {
			return err
		}
		router, err := s.buildBinary("router", filepath.Join(s.root, "router"), "./cmd/router", observationDigest)
		if err != nil {
			return err
		}
		marshaller, err := s.buildBinary("marshaller", filepath.Join(s.root, "marshaller"), "./cmd/marshaller", observationDigest)
		if err != nil {
			return err
		}
		if err := s.spawn(state, "router", []string{router},
			instance(s.processEnv, "router-local"), filepath.Join(s.root, "router")); err != nil {
			return err
		}
		if err := ready.Wait(s.url(env, "ROUTER_HTTP_PORT", "/ready"), readinessTimeout,
			env.token["ROUTER_API_TOKEN"], "Router", s.alive(state, "router")); err != nil {
			return err
		}
		if err := s.spawn(state, "marshaller", []string{marshaller},
			instance(s.processEnv, "marshaller-local"), filepath.Join(s.root, "marshaller")); err != nil {
			return err
		}
		if err := ready.Wait(s.url(env, "MARSHALLER_HTTP_PORT", "/ready"), readinessTimeout,
			env.token["MARSHALLER_API_TOKEN"], "Marshaller", s.alive(state, "marshaller")); err != nil {
			return err
		}
	}

	if err := s.spawn(state, "backend", []string{backend}, instance(s.processEnv, "backend-local"), backendDir); err != nil {
		return err
	}
	if err := ready.Wait(s.url(env, "HTTP_PORT", "/ready"), readinessTimeout, "",
		"Backend", s.alive(state, "backend")); err != nil {
		return err
	}
	if err := s.spawn(state, "business-worker", []string{worker},
		instance(s.processEnv, "business-worker-local"), backendDir); err != nil {
		return err
	}
	if err := ready.Wait("http://127.0.0.1:19102/ready", readinessTimeout, "",
		"Business Worker", s.alive(state, "business-worker")); err != nil {
		return err
	}
	if err := s.spawn(state, "search-indexer", []string{indexer},
		instance(s.processEnv, "search-indexer-local"), backendDir); err != nil {
		return err
	}
	if err := ready.Wait("http://127.0.0.1:19103/ready", readinessTimeout, "",
		"Search Indexer", s.alive(state, "search-indexer")); err != nil {
		return err
	}

	if err := s.ensureNpmDependencies("frontend"); err != nil {
		return err
	}
	frontendDir := filepath.Join(s.root, "frontend")
	if err := s.spawn(state, "frontend", []string{"npm", "run", "dev", "--", "--host", "127.0.0.1", "--port", s.values["FRONTEND_PORT"]},
		copyEnvironment(s.processEnv), frontendDir); err != nil {
		return err
	}
	if err := ready.Wait(s.url(env, "FRONTEND_PORT", "/"), readinessTimeout, "",
		"user Vite", s.alive(state, "frontend")); err != nil {
		return err
	}

	if observe {
		if err := s.ensureNpmDependencies("admin-frontend"); err != nil {
			return err
		}
		adminDir := filepath.Join(s.root, "admin-frontend")
		adminPort := s.values["ADMIN_FRONTEND_PORT"]
		adminEnv := copyEnvironment(s.processEnv)
		adminEnv["FRONTEND_PORT"] = adminPort
		if err := s.spawn(state, "admin-frontend",
			[]string{"npm", "run", "dev", "--", "--host", "127.0.0.1", "--port", adminPort}, adminEnv, adminDir); err != nil {
			return err
		}
		if err := ready.Wait(fmt.Sprintf("http://127.0.0.1:%s/admin/", adminPort), readinessTimeout, "",
			"admin Vite", s.alive(state, "admin-frontend")); err != nil {
			return err
		}
		if err := ready.Wait(s.url(env, "MONITOR_HTTP_PORT", "/ready"), readinessTimeout,
			env.token["MONITOR_API_TOKEN"], "Monitor", nil); err != nil {
			return err
		}
	}
	return nil
}

// buildBinary reuses a locally built binary while its source digest is
// unchanged and otherwise rebuilds it into the private bin directory.
func (s *session) buildBinary(name, directory, pkg, sourceDigest string) (string, error) {
	binary := filepath.Join(s.ws.BinRoot(), name)
	metadata := filepath.Join(s.ws.BinRoot(), name+".json")
	previous := struct {
		Digest string `json:"digest"`
	}{}
	if raw, err := os.ReadFile(metadata); err == nil {
		_ = json.Unmarshal(raw, &previous)
	}
	if info, err := os.Stat(binary); err == nil && !info.IsDir() && previous.Digest == sourceDigest {
		return binary, nil
	}
	if err := os.MkdirAll(s.ws.BinRoot(), 0o755); err != nil {
		return "", err
	}
	command := []string{"go", "build", "-trimpath", "-o", binary, pkg}
	if err := proc.Run(command, directory, s.processEnv, "build "+name, 0); err != nil {
		return "", err
	}
	if err := workspace.SaveJSON(metadata, map[string]any{
		"name": name, "digest": sourceDigest, "command": command,
	}); err != nil {
		return "", err
	}
	return binary, nil
}

func (s *session) spawn(state *workspace.State, name string, command []string, env map[string]string, cwd string) error {
	record, err := proc.Spawn(filepath.Join(s.ws.LogRoot(), name+".log"), cwd, command, env, now())
	if err != nil {
		return err
	}
	state.Processes[name] = record
	return workspace.Save(s.ws, state)
}

func (s *session) alive(state *workspace.State, name string) func() bool {
	return func() bool {
		record, found := state.Processes[name]
		return found && proc.Owned(record)
	}
}

func (s *session) url(env environment, key, path string) string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", env.integer(key), path)
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

func copyEnvironment(source map[string]string) map[string]string {
	target := make(map[string]string, len(source))
	for key, value := range source {
		target[key] = value
	}
	return target
}
