//go:build integration

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const migrationOwnerLabel = "io.gopulse.migration-owner"

type migrationStateReceipt struct {
	Schema       string   `json:"schema"`
	Complete     bool     `json:"complete"`
	Checks       []string `json:"checks"`
	BinaryTarget int      `json:"binary_target,omitempty"`
	Cleanup      string   `json:"cleanup,omitempty"`
}

type migrationCLIOutput struct {
	BinaryTarget    int    `json:"binary_target"`
	DatabaseVersion int    `json:"database_version"`
	State           string `json:"state"`
	Changed         *bool  `json:"changed"`
	Reason          string `json:"reason"`
	ExitCode        int    `json:"exit_code"`
}

type migrationCommandResult struct {
	stdout, stderr string
	code           int
	err            error
}

type migrationContainerInfo struct {
	Labels map[string]string `json:"labels"`
	Ports  map[string][]struct {
		HostIP   string `json:"HostIp"`
		HostPort string `json:"HostPort"`
	} `json:"ports"`
	Mounts []struct {
		Type string `json:"Type"`
		Name string `json:"Name"`
	} `json:"mounts"`
}

type migrationFixture struct {
	t                         *testing.T
	ctx                       context.Context
	backend, binary, password string
	name, owner, container    string
	env                       []string
	receipt                   migrationStateReceipt
	checksPassed, cleaned     bool
}

// This fixture owns its entire schema and never consumes the shared business
// integration database. Docker and dependency failures are acceptance failures.
func TestMigrationStateIntegration(t *testing.T) {
	f := &migrationFixture{t: t, receipt: migrationStateReceipt{
		Schema: "gopulse.migration-state.v1", Checks: []string{},
	}}
	// Registered first so the receipt follows process, container and temp cleanup.
	t.Cleanup(func() {
		f.receipt.Complete = f.checksPassed && f.cleaned && !t.Failed()
		data, err := json.Marshal(f.receipt)
		if err != nil {
			t.Errorf("encode migration receipt: %v", err)
			return
		}
		t.Log(string(data))
	})
	if os.Getenv("INTEGRATION_TESTS") != "1" {
		t.Fatal("migration acceptance requires INTEGRATION_TESTS=1")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("migration acceptance requires Docker")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute+30*time.Second)
	f.ctx = ctx
	t.Cleanup(cancel)
	f.requireCommand("Docker availability", f.command(ctx, 15*time.Second, nil, "", "docker", "info", "--format", "{{.ServerVersion}}"))

	work := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal("locate migrate package working directory")
	}
	f.backend = filepath.Clean(filepath.Join(cwd, "..", ".."))
	f.binary = filepath.Join(work, "migrate")
	locker := filepath.Join(work, "migration-lock")
	f.requireCommand("build migrate", f.command(ctx, 90*time.Second, nil, "", "go", "build", "-o", f.binary, "./cmd/migrate"))
	f.requireCommand("build lock fixture", f.command(ctx, 90*time.Second, nil, "", "go", "build", "-o", locker, filepath.Join(f.backend, "testdata", "migration-lock.go")))

	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		t.Fatal("generate migration fixture identity")
	}
	f.owner = hex.EncodeToString(token[:])
	f.name = "gopulse-migration-" + f.owner[:12]
	f.password = "migration-canary-" + f.owner
	envfile := filepath.Join(work, "mysql.env")
	values := "MYSQL_ROOT_PASSWORD=" + f.password + "\nMYSQL_DATABASE=gopulse\nMYSQL_USER=gopulse\nMYSQL_PASSWORD=" + f.password + "\n"
	if err := os.WriteFile(envfile, []byte(values), 0600); err != nil {
		t.Fatal("write private MySQL environment")
	}
	info, err := os.Stat(envfile)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("MySQL environment must have mode 0600")
	}
	// Name-based cleanup is installed before run, including partial start failure.
	t.Cleanup(f.cleanup)
	f.container = strings.TrimSpace(f.requireCommand("start owned MySQL", f.command(ctx, 2*time.Minute, nil, "", "docker", "run", "-d", "--name", f.name,
		"--label", migrationOwnerLabel+"="+f.owner, "--env-file", envfile, "-p", "127.0.0.1::3306", "mysql:8.4.0")))
	container := f.inspect(ctx)
	bindings := container.Ports["3306/tcp"]
	if container.Labels[migrationOwnerLabel] != f.owner || len(bindings) != 1 || bindings[0].HostIP != "127.0.0.1" {
		t.Fatal("owned MySQL must have its owner label and one loopback binding")
	}
	port, err := strconv.Atoi(bindings[0].HostPort)
	if err != nil || port < 1 || port > 65535 {
		t.Fatal("owned MySQL did not receive a valid dynamic port")
	}
	overrides := map[string]string{
		"GOPULSE_RUNTIME_MODE": "host", "MYSQL_HOST": "127.0.0.1", "MYSQL_PORT": bindings[0].HostPort,
		"MYSQL_DATABASE": "gopulse", "MYSQL_USER": "gopulse", "MYSQL_PASSWORD": f.password,
	}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if _, replaced := overrides[key]; !replaced {
			f.env = append(f.env, value)
		}
	}
	for key, value := range overrides {
		f.env = append(f.env, key+"="+value)
	}
	f.awaitMySQL()

	validated := f.migrate("validate", 0)
	if validated.State != "valid" || validated.BinaryTarget != 13 {
		t.Fatal("expected valid migration source with the contracted target 13")
	}
	f.receipt.BinaryTarget = validated.BinaryTarget
	f.requireState(f.migrate("status", 0), "clean", -1)
	results := make(chan migrationCommandResult, 2)
	for range 2 {
		go func() { results <- f.command(ctx, 45*time.Second, f.env, "", f.binary, "up") }()
	}
	changed := 0
	for range 2 {
		output := f.decodeMigration(<-results, 0)
		f.requireState(output, "current", validated.BinaryTarget)
		if output.Changed == nil {
			t.Fatal("concurrent up omitted changed")
		}
		if *output.Changed {
			changed++
		}
	}
	if changed != 1 {
		t.Fatalf("concurrent up changed count = %d; want 1", changed)
	}
	f.requireState(f.migrate("status", 0), "current", validated.BinaryTarget)
	repeated := f.migrate("up", 0)
	f.requireState(repeated, "current", validated.BinaryTarget)
	if repeated.Changed == nil || *repeated.Changed {
		t.Fatal("repeated up must report changed=false")
	}
	f.receipt.Checks = append(f.receipt.Checks, "empty_concurrent_current_repeat")

	f.checkLockTimeout(locker)
	f.receipt.Checks = append(f.receipt.Checks, "lock_timeout")
	baseline := f.sql("SHOW TABLES;")
	f.sql("INSERT INTO users (username,password_hash) VALUES ('migration-sentinel','sentinel-hash');")
	dataQuery := "SELECT id,username,password_hash,role,created_at,updated_at,display_name,bio FROM users ORDER BY id;"
	baselineData := f.sql(dataQuery)
	for _, rejected := range []struct {
		version, code int
		state         string
		dirty         int
	}{{11, 4, "dirty", 1}, {validated.BinaryTarget + 1, 5, "ahead", 0}} {
		f.sql(fmt.Sprintf("UPDATE schema_migrations SET version=%d,dirty=%d;", rejected.version, rejected.dirty))
		f.requireState(f.migrate("status", rejected.code), rejected.state, rejected.version)
		if f.migrate("up", rejected.code).Reason != rejected.state {
			t.Fatalf("expected %s refusal", rejected.state)
		}
		if f.sql("SHOW TABLES;") != baseline || f.sql(dataQuery) != baselineData ||
			f.sql("SELECT version,dirty FROM schema_migrations;") != fmt.Sprintf("%d\t%d", rejected.version, rejected.dirty) {
			t.Fatalf("%s refusal changed schema, data or migration metadata", rejected.state)
		}
		f.receipt.Checks = append(f.receipt.Checks, rejected.state+"_rejected_unchanged")
	}

	f.sql(fmt.Sprintf("UPDATE schema_migrations SET version=%d,dirty=0;", validated.BinaryTarget))
	// Local-only reverse setup; this is not a product rollback/recovery mechanism.
	f.migrate("down", 0)
	f.requireState(f.migrate("status", 0), "behind", 12)
	beforeFailedApply := f.sql("SHOW TABLES;")
	f.sql("REVOKE CREATE ON gopulse.* FROM 'gopulse'@'%';")
	if f.migrate("up", 8).Reason != "apply_failure" {
		t.Fatal("DDL permission failure must report apply_failure")
	}
	f.requireState(f.migrate("status", 4), "dirty", validated.BinaryTarget)
	if f.sql("SHOW TABLES;") != beforeFailedApply || f.sql(dataQuery) != baselineData ||
		f.sql("SELECT version,dirty FROM schema_migrations;") != "13\t1" {
		t.Fatal("DDL permission failure must preserve data and the dirty marker")
	}
	if f.migrate("up", 4).Reason != "dirty" {
		t.Fatal("failed apply must remain rejected as dirty")
	}
	f.receipt.Checks = append(f.receipt.Checks, "apply_failure_keeps_dirty")
	f.sql("GRANT CREATE ON gopulse.* TO 'gopulse'@'%';")
	f.sql("UPDATE schema_migrations SET version=12,dirty=1;")
	f.requireState(f.migrate("status", 4), "dirty", 12)
	recovered := f.migrate("up", 0)
	f.requireState(recovered, "current", validated.BinaryTarget)
	if recovered.Changed == nil || !*recovered.Changed || f.sql("SHOW TABLES;") != baseline || f.sql(dataQuery) != baselineData ||
		f.sql("SELECT version,dirty FROM schema_migrations;") != "13\t0" {
		t.Fatal("explicit v12 resume must restore target 13 and preserve existing data")
	}
	f.receipt.Checks = append(f.receipt.Checks, "explicit_v12_resume_then_v13")
	f.checksPassed = true
}

func (f *migrationFixture) command(parent context.Context, timeout time.Duration, env []string, input, executable string, args ...string) migrationCommandResult {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir, cmd.Env, cmd.Stdin = f.backend, env, strings.NewReader(input)
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		code = -1
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
	}
	return migrationCommandResult{stdout.String(), stderr.String(), code, err}
}

func (f *migrationFixture) safe(output string) string {
	if f.password != "" {
		return strings.ReplaceAll(output, f.password, "[REDACTED]")
	}
	return output
}

func (f *migrationFixture) requireCommand(label string, result migrationCommandResult) string {
	f.t.Helper()
	if result.err != nil {
		f.t.Fatalf("%s failed (exit %d): %s", label, result.code, f.safe(result.stdout+result.stderr))
	}
	return result.stdout
}

func (f *migrationFixture) migrate(action string, code int) migrationCLIOutput {
	f.t.Helper()
	return f.decodeMigration(f.command(f.ctx, 45*time.Second, f.env, "", f.binary, action), code)
}

func (f *migrationFixture) decodeMigration(result migrationCommandResult, code int) migrationCLIOutput {
	f.t.Helper()
	if strings.Contains(result.stdout+result.stderr, f.password) {
		f.t.Fatal("migration CLI leaked fixture credentials")
	}
	if result.code != code || (code == 0 && result.err != nil) {
		f.t.Fatalf("migration exit = %d; want %d; output: %s", result.code, code, f.safe(result.stdout+result.stderr))
	}
	if code != 0 {
		var failure migrationCLIOutput
		if err := json.Unmarshal([]byte(result.stderr), &failure); err != nil || failure.ExitCode != code || failure.Reason == "" {
			f.t.Fatalf("migration failure must have a safe reason and exit_code=%d", code)
		}
	}
	data := result.stdout
	if strings.TrimSpace(data) == "" {
		data = result.stderr
	}
	var output migrationCLIOutput
	if strings.TrimSpace(data) != "" {
		if err := json.Unmarshal([]byte(data), &output); err != nil {
			f.t.Fatalf("invalid migration JSON: %s", f.safe(data))
		}
	}
	return output
}

func (f *migrationFixture) requireState(output migrationCLIOutput, state string, version int) {
	f.t.Helper()
	if output.State != state || output.DatabaseVersion != version || output.BinaryTarget != f.receipt.BinaryTarget {
		f.t.Fatalf("migration state = %s/%d/target%d; want %s/%d/target%d", output.State, output.DatabaseVersion, output.BinaryTarget, state, version, f.receipt.BinaryTarget)
	}
}

func (f *migrationFixture) sqlResult(ctx context.Context, statement string) migrationCommandResult {
	return f.command(ctx, 10*time.Second, nil, statement, "docker", "exec", "-i", f.container, "sh", "-c",
		`MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot -N -B gopulse`)
}

func (f *migrationFixture) sql(statement string) string {
	f.t.Helper()
	return strings.TrimSpace(f.requireCommand("owned MySQL statement", f.sqlResult(f.ctx, statement)))
}

func (f *migrationFixture) awaitMySQL() {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 90*time.Second)
	defer cancel()
	for {
		result := f.sqlResult(ctx, "SELECT 1;")
		if result.err == nil && strings.TrimSpace(result.stdout) == "1" {
			return
		}
		select {
		case <-ctx.Done():
			f.t.Fatalf("owned MySQL did not become ready: %s", f.safe(result.stderr))
		case <-time.After(time.Second):
		}
	}
}

func (f *migrationFixture) inspect(ctx context.Context) migrationContainerInfo {
	f.t.Helper()
	// Inspect only non-secret fields; the full Docker Config contains passwords.
	format := `{"labels":{{json .Config.Labels}},"ports":{{json .NetworkSettings.Ports}},"mounts":{{json .Mounts}}}`
	data := f.requireCommand("inspect owned MySQL", f.command(ctx, 10*time.Second, nil, "", "docker", "inspect", "--format", format, f.name))
	var info migrationContainerInfo
	if err := json.Unmarshal([]byte(data), &info); err != nil {
		f.t.Fatal("invalid owned MySQL metadata")
	}
	return info
}

func (f *migrationFixture) cleanup() {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ids := strings.TrimSpace(f.requireCommand("locate owned cleanup resource", f.command(ctx, 10*time.Second, nil, "", "docker", "ps", "-aq", "--filter", "name=^/"+f.name+"$")))
	if ids == "" {
		if f.container != "" {
			f.t.Error("owned MySQL disappeared before its cleanup assertion")
		}
		return
	}
	info := f.inspect(ctx)
	if info.Labels[migrationOwnerLabel] != f.owner {
		f.t.Error("refusing MySQL cleanup: owner label mismatch")
		return
	}
	f.requireCommand("remove owned MySQL and anonymous volumes", f.command(ctx, 15*time.Second, nil, "", "docker", "rm", "-fv", f.name))
	remaining := f.requireCommand("verify MySQL removal", f.command(ctx, 5*time.Second, nil, "", "docker", "ps", "-aq", "--filter", "name=^/"+f.name+"$"))
	if strings.TrimSpace(remaining) != "" {
		f.t.Error("owned MySQL remains after cleanup")
		return
	}
	for _, mount := range info.Mounts {
		if mount.Type != "volume" {
			continue
		}
		remaining := f.requireCommand("verify anonymous volume removal", f.command(ctx, 5*time.Second, nil, "", "docker", "volume", "ls", "-q", "--filter", "name=^"+mount.Name+"$"))
		if strings.TrimSpace(remaining) != "" {
			f.t.Error("owned MySQL anonymous volume remains after cleanup")
			return
		}
	}
	f.cleaned = true
	f.receipt.Cleanup = "owned_container_and_anonymous_volumes_removed"
}

func (f *migrationFixture) checkLockTimeout(executable string) {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable)
	cmd.Env, cmd.Dir, cmd.WaitDelay = f.env, f.backend, 2*time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		f.t.Fatal("create migration lock fixture output")
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		f.t.Fatal("start migration lock fixture")
	}
	waited := false
	defer func() {
		if !waited {
			cancel()
			_ = cmd.Wait()
		}
	}()
	ready, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || ready != "locked\n" {
		f.t.Fatal("migration lock fixture did not acquire the lock")
	}
	if f.migrate("up", 6).Reason != "lock_timeout" {
		f.t.Fatal("held migration lock must return lock_timeout")
	}
	err = cmd.Wait()
	waited = true
	if err != nil {
		f.t.Fatalf("migration lock fixture failed: %s", f.safe(stderr.String()))
	}
	if strings.Contains(ready+stderr.String(), f.password) {
		f.t.Fatal("migration lock fixture leaked credentials")
	}
}
