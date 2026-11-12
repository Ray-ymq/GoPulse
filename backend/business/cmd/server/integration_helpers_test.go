//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	integrationHTTPReadyTimeout = 30 * time.Second
	integrationShutdownTimeout  = 5 * time.Second
	integrationCommandTimeout   = 90 * time.Second
)

type integrationServer struct {
	cmd     *exec.Cmd
	done    chan error
	address string
	logs    *bytes.Buffer
	stopped bool
}

func startIntegrationServer(t *testing.T, binary, backendDir string, environment []string) *integrationServer {
	t.Helper()
	port := freeIntegrationPort(t)
	setEnvironment(&environment, "HTTP_PORT", strconv.Itoa(port))
	logs := &bytes.Buffer{}
	cmd := exec.Command(binary)
	cmd.Dir = backendDir
	cmd.Env = environment
	cmd.Stdout = logs
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("start backend server: %v", err)
	}
	server := &integrationServer{cmd: cmd, done: waitIntegrationProcess(cmd), address: "http://127.0.0.1:" + strconv.Itoa(port), logs: logs}
	t.Cleanup(func() {
		if server.stopped || server.cmd.ProcessState != nil {
			return
		}
		server.stopped = true
		_ = server.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-server.done:
		case <-time.After(integrationShutdownTimeout):
			_ = server.cmd.Process.Kill()
			<-server.done
		}
	})
	return server
}

func waitIntegrationProcess(cmd *exec.Cmd) chan error {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	return done
}

func stopIntegrationServer(t *testing.T, server *integrationServer) {
	t.Helper()
	if server.stopped {
		return
	}
	if err := server.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("stop backend server: %v; logs=%s", err, server.logs.String())
	}
	select {
	case err := <-server.done:
		if err != nil {
			t.Fatalf("backend server shutdown error: %v; logs=%s", err, server.logs.String())
		}
	case <-time.After(integrationShutdownTimeout):
		t.Fatalf("backend server did not exit within %s; logs=%s", integrationShutdownTimeout, server.logs.String())
	}
	server.stopped = true
}

func waitIntegrationReady(t *testing.T, server *integrationServer, client *http.Client) {
	t.Helper()
	deadline := time.Now().Add(integrationHTTPReadyTimeout)
	for time.Now().Before(deadline) {
		select {
		case err := <-server.done:
			t.Fatalf("backend server exited before ready: %v; logs=%s", err, server.logs.String())
		default:
		}
		request, err := http.NewRequest(http.MethodGet, server.address+"/ready", nil)
		if err == nil {
			response, requestErr := client.Do(request)
			if requestErr == nil {
				body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
				_ = response.Body.Close()
				if response.StatusCode == http.StatusOK {
					return
				}
				_ = body
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("backend server did not become ready within %s; logs=%s", integrationHTTPReadyTimeout, server.logs.String())
}

func registerIntegrationUser(t *testing.T, client *http.Client, address, username, password string) uint64 {
	t.Helper()
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		t.Fatalf("encode registration: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, address+"/api/v1/auth/register", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create registration request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("register integration user: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("register integration user status = %d, want 201; body=%s", response.StatusCode, readIntegrationBody(response))
	}
	var result struct {
		Data struct {
			ID uint64 `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode registration response: %v", err)
	}
	if result.Data.ID == 0 {
		t.Fatal("registration returned no user ID")
	}
	return result.Data.ID
}

func integrationStatus(t *testing.T, client *http.Client, address string) int {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		t.Fatalf("create status request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("request %s: %v", address, err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	return response.StatusCode
}

func readIntegrationBody(response *http.Response) string {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	return string(body)
}

func runIntegrationCommand(t *testing.T, dir string, timeout time.Duration, environment []string, name string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	command.Env = environment
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v; output=%s", name, strings.Join(args, " "), err, output)
	}
}

func findBackendDir(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get integration working directory: %v", err)
	}
	for index := 0; index < 4; index++ {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		directory = filepath.Dir(directory)
	}
	t.Fatal("backend go.mod was not found")
	return ""
}

func integrationEnvironment(t *testing.T) []string {
	t.Helper()
	environment := append([]string(nil), os.Environ()...)
	setEnvironment(&environment, "INTEGRATION_TESTS", "1")
	setEnvironment(&environment, "APP_ENV", "test")
	setEnvironment(&environment, "GOPULSE_RUNTIME_MODE", "host")
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		setDefaultEnvironment(&environment, "MYSQL_HOST", "127.0.0.1")
		setDefaultEnvironment(&environment, "MYSQL_PORT", "3306")
		setDefaultEnvironment(&environment, "MYSQL_DATABASE", "gopulse_integration")
		setDefaultEnvironment(&environment, "MYSQL_USER", "gopulse_integration")
		setDefaultEnvironment(&environment, "MYSQL_PASSWORD", "integration-mysql")
		setDefaultEnvironment(&environment, "REDIS_HOST", "127.0.0.1")
		setDefaultEnvironment(&environment, "REDIS_PORT", "6379")
		setDefaultEnvironment(&environment, "REDIS_PASSWORD", "integration-redis")
		setDefaultEnvironment(&environment, "REDIS_DB", "15")
		setDefaultEnvironment(&environment, "RABBITMQ_URL", "amqp://integration:integration@127.0.0.1:5672/")
		setDefaultEnvironment(&environment, "ELASTICSEARCH_URL", "http://127.0.0.1:9200")
	} else {
		setDefaultEnvironment(&environment, "MYSQL_HOST", "127.0.0.1")
		setDefaultEnvironment(&environment, "MYSQL_PORT", "23306")
		setDefaultEnvironment(&environment, "MYSQL_DATABASE", "gopulse_integration")
		setDefaultEnvironment(&environment, "MYSQL_USER", "gopulse_integration")
		setDefaultEnvironment(&environment, "MYSQL_PASSWORD", "integration-mysql")
		setDefaultEnvironment(&environment, "REDIS_HOST", "127.0.0.1")
		setDefaultEnvironment(&environment, "REDIS_PORT", "26379")
		setDefaultEnvironment(&environment, "REDIS_PASSWORD", "integration-redis")
		setDefaultEnvironment(&environment, "REDIS_DB", "15")
		setDefaultEnvironment(&environment, "RABBITMQ_URL", "amqp://gopulse_integration:integration-rabbitmq@127.0.0.1:25672/")
		setDefaultEnvironment(&environment, "ELASTICSEARCH_URL", "http://127.0.0.1:29200")
	}
	setDefaultEnvironment(&environment, "MYSQL_MAX_IDLE_CONNS", "2")
	setDefaultEnvironment(&environment, "MYSQL_TOTAL_MAX_OPEN_CONNS", "60")
	setDefaultEnvironment(&environment, "AUTH_JWT_SECRET", "integration-jwt-secret-at-least-32-bytes-long")
	setDefaultEnvironment(&environment, "AUTH_JWT_TTL", "2h")
	setDefaultEnvironment(&environment, "AUTH_COOKIE_NAME", "gopulse_integration_session")
	setDefaultEnvironment(&environment, "AUTH_COOKIE_SECURE", "false")
	return environment
}

func roleEnvironment(base []string, role string, concurrency int) []string {
	environment := append([]string(nil), base...)
	setEnvironment(&environment, "BACKEND_SERVICE_ROLE", role)
	setEnvironment(&environment, "GOPULSE_INSTANCE_ID", "backend-role")
	setEnvironment(&environment, "BACKEND_HTTP_MAX_CONCURRENCY", strconv.Itoa(concurrency))
	setEnvironment(&environment, "MYSQL_MAX_OPEN_CONNS", "8")
	return environment
}

func freeIntegrationPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate integration HTTP port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release integration HTTP port: %v", err)
	}
	return port
}

func setEnvironment(environment *[]string, key, value string) {
	unsetEnvironment(environment, key)
	*environment = append(*environment, key+"="+value)
}

func setDefaultEnvironment(environment *[]string, key, value string) {
	for _, entry := range *environment {
		if strings.HasPrefix(entry, key+"=") {
			return
		}
	}
	setEnvironment(environment, key, value)
}

func unsetEnvironment(environment *[]string, key string) {
	values := *environment
	for index := len(values) - 1; index >= 0; index-- {
		if strings.HasPrefix(values[index], key+"=") {
			values = append(values[:index], values[index+1:]...)
		}
	}
	*environment = values
}
